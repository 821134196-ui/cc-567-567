package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func registerRoutes(r *gin.Engine) {
	r.Static("/uploads", UploadDir)

	api := r.Group("/api")
	{
		api.POST("/login", login)

		authed := api.Group("")
		authed.Use(authRequired())
		{
			authed.GET("/me", me)
			authed.POST("/uploads", uploadImage)

			authed.GET("/orders", listOrders)
			authed.GET("/orders/:id", getOrder)

			// 测量员
			surveyor := authed.Group("")
			surveyor.Use(rolesRequired(RoleSurveyor))
			{
				surveyor.POST("/orders", createOrder)
				surveyor.POST("/orders/:id/versions", createVersion)
				surveyor.POST("/orders/:id/measurements", submitInitialMeasurements)
				surveyor.POST("/versions/:vid/submit", submitVersion)
				surveyor.POST("/orders/:id/openings/:slot/remeasure", remeasureOpening)
			}

			// 客户
			customer := authed.Group("")
			customer.Use(rolesRequired(RoleCustomer))
			{
				customer.POST("/versions/:vid/confirm", confirmVersion)
				customer.POST("/versions/:vid/reject", rejectVersion)
				customer.POST("/reworks/:rid/confirm", confirmRework)
				customer.POST("/reworks/:rid/reject", rejectRework)
				customer.POST("/orders/:id/accept", acceptOrder)
			}

			// 工厂
			factory := authed.Group("")
			factory.Use(rolesRequired(RoleFactory))
			{
				factory.POST("/orders/:id/production/start", startProduction)
				factory.POST("/orders/:id/production/finish", finishProduction)
				factory.POST("/reworks/:rid/execute", executeRework)
			}

			// 安装人员
			installer := authed.Group("")
			installer.Use(rolesRequired(RoleInstaller))
			{
				installer.POST("/orders/:id/site-measurements", submitSiteMeasurement)
				installer.POST("/deviations/:did/propose", proposeRework)
				installer.POST("/deviations/:did/resolve", resolveDeviation)
				installer.POST("/orders/:id/install", installOrder)
			}

			// 费用记录：三个岗位均可登记，客户只读
			staff := authed.Group("")
			staff.Use(rolesRequired(RoleSurveyor, RoleFactory, RoleInstaller))
			staff.POST("/orders/:id/costs", addCost)
		}
	}
}

func login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var u User
	err := db.Where("username = ?", req.Username).First(&u).Error
	if err != nil || !checkPassword(u.PasswordHash, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token": issueToken(u.ID),
		"user":  gin.H{"id": u.ID, "name": u.Name, "role": u.Role},
	})
}

func me(c *gin.Context) {
	u := c.MustGet("user").(*authUser)
	c.JSON(http.StatusOK, gin.H{"id": u.ID, "name": u.Name, "role": u.Role})
}

func getOrderByID(c *gin.Context) (*Order, bool) {
	var o Order
	if err := db.First(&o, c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "工单不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return nil, false
	}
	return &o, true
}

func listOrders(c *gin.Context) {
	var orders []Order
	q := db.Order("id desc")
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Find(&orders).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, orders)
}

func createOrder(c *gin.Context) {
	u := c.MustGet("user").(*authUser)
	var req struct {
		Code         string `json:"code"`
		CustomerName string `json:"customer_name"`
		Address      string `json:"address"`
		Note         string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "工单号不能为空"})
		return
	}
	var customer User
	if err := db.Where("role = ?", RoleCustomer).First(&customer).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "系统中没有客户账号"})
		return
	}
	o := Order{
		Code: req.Code, CustomerID: customer.ID, CustomerName: req.CustomerName,
		Address: req.Address, Note: req.Note, Status: StatusMeasuring,
		CreatedByID: u.ID, CreatedByName: u.Name,
	}
	if o.CustomerName == "" {
		o.CustomerName = customer.Name
	}
	if err := db.Create(&o).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "工单号已存在"})
		return
	}
	c.JSON(http.StatusCreated, o)
}

type timelineItem struct {
	At     time.Time `json:"at"`
	Type   string    `json:"type"`
	Title  string    `json:"title"`
	Detail string    `json:"detail"`
	Actor  string    `json:"actor"`
}

type orderDetail struct {
	Order
	Versions      []DrawingVersion    `json:"versions"`
	Measures      []MeasureRecord     `json:"measures"`
	Confirmations []Confirmation      `json:"confirmations"`
	Productions   []ProductionRecord  `json:"productions"`
	Deviations    []Deviation         `json:"deviations"`
	Costs         []CostRecord        `json:"costs"`
	Installation  *InstallationRecord `json:"installation"`
	Acceptance    *AcceptanceRecord   `json:"acceptance"`
	Timeline      []timelineItem      `json:"timeline"`
}

func getOrder(c *gin.Context) {
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	var versions []DrawingVersion
	db.Where("order_id = ?", o.ID).Order("version_no asc").Find(&versions)
	versionIDs := make([]uint, 0, len(versions))
	for i := range versions {
		vid := versions[i].ID
		versionIDs = append(versionIDs, vid)
		var openings []Opening
		db.Where("version_id = ?", vid).Order("slot asc").Find(&openings)
		versions[i].Openings = openings
	}

	var measures []MeasureRecord
	db.Where("order_id = ?", o.ID).Order("created_at asc").Find(&measures)

	var confirmations []Confirmation
	db.Where("order_id = ?", o.ID).Order("created_at asc").Find(&confirmations)

	var productions []ProductionRecord
	db.Where("order_id = ?", o.ID).Order("created_at asc").Find(&productions)

	var deviations []Deviation
	db.Where("order_id = ?", o.ID).Order("created_at asc").Find(&deviations)
	devIDs := make([]uint, 0, len(deviations))
	for i := range deviations {
		devIDs = append(devIDs, deviations[i].ID)
	}
	if len(devIDs) > 0 {
		var reworks []ReworkEvent
		db.Where("deviation_id IN ?", devIDs).Order("created_at asc").Find(&reworks)
		byDev := map[uint][]ReworkEvent{}
		for _, rw := range reworks {
			byDev[rw.DeviationID] = append(byDev[rw.DeviationID], rw)
		}
		for i := range deviations {
			deviations[i].ReworkEvents = byDev[deviations[i].ID]
		}
	}

	var costs []CostRecord
	db.Where("order_id = ?", o.ID).Order("created_at asc").Find(&costs)

	var inst InstallationRecord
	var instPtr *InstallationRecord
	if err := db.Where("order_id = ?", o.ID).First(&inst).Error; err == nil {
		instPtr = &inst
	}
	var acc AcceptanceRecord
	var accPtr *AcceptanceRecord
	if err := db.Where("order_id = ?", o.ID).First(&acc).Error; err == nil {
		accPtr = &acc
	}

	c.JSON(http.StatusOK, orderDetail{
		Order: *o, Versions: versions, Measures: measures,
		Confirmations: confirmations, Productions: productions,
		Deviations: deviations, Costs: costs,
		Installation: instPtr, Acceptance: accPtr,
		Timeline: buildTimeline(o, versions, measures, confirmations, productions, deviations, costs, instPtr, accPtr),
	})
}

func buildTimeline(o *Order, versions []DrawingVersion, measures []MeasureRecord,
	confs []Confirmation, prods []ProductionRecord, devs []Deviation,
	costs []CostRecord, inst *InstallationRecord, acc *AcceptanceRecord) []timelineItem {
	tl := []timelineItem{}
	add := func(t time.Time, typ, title, detail, actor string) {
		tl = append(tl, timelineItem{At: t, Type: typ, Title: title, Detail: detail, Actor: actor})
	}
	add(o.CreatedAt, "order", "工单创建", o.Code, o.CreatedByName)
	for _, v := range versions {
		add(v.CreatedAt, "version", "图纸 V"+itoa(v.VersionNo)+" 创建", v.Note, v.CreatedByName)
		if v.SubmittedAt != nil {
			add(*v.SubmittedAt, "version", "图纸 V"+itoa(v.VersionNo)+" 提交客户", "", v.CreatedByName)
		}
		if v.ConfirmedAt != nil {
			add(*v.ConfirmedAt, "confirm", "图纸 V"+itoa(v.VersionNo)+" 客户签字确认", "", v.ConfirmedByName)
		}
	}
	stageName := map[string]string{
		MeasureInitial: "初测", MeasureRemeasure: "复测", MeasureFactoryActual: "工厂实际开孔",
		MeasureSite: "现场测量", MeasurePostRework: "返工后复测",
	}
	for _, m := range measures {
		detail := ""
		if m.Stage == MeasureRemeasure && m.Reason != "" {
			detail = "原因：" + m.Reason
		}
		if m.Note != "" {
			if detail != "" {
				detail += "；"
			}
			detail += m.Note
		}
		add(m.CreatedAt, "measure", stageName[m.Stage]+"："+m.Slot, detail, m.MeasuredByName)
	}
	for _, cf := range confs {
		title := "客户确认图纸 V" + itoa(cf.VersionNo)
		if cf.Action == "reject" {
			title = "客户驳回图纸 V" + itoa(cf.VersionNo)
		}
		add(cf.CreatedAt, "confirm", title, cf.Note, cf.SignedName)
	}
	for _, p := range prods {
		if p.StartedAt != nil {
			add(*p.StartedAt, "production", "工厂开始加工（V"+itoa(p.VersionNo)+"）", p.Note, p.OperatorName)
		}
		if p.FinishedAt != nil {
			add(*p.FinishedAt, "production", "加工完成（V"+itoa(p.VersionNo)+"）", p.Note, p.OperatorName)
		}
	}
	for _, d := range devs {
		add(d.CreatedAt, "deviation", "发现开孔超差："+d.Slot, d.Note, d.FoundByName)
		for _, rw := range d.ReworkEvents {
			add(rw.CreatedAt, "rework", "提出"+reworkTypeName(rw.Type)+"方案", rw.Plan, rw.ProposedByName)
			if rw.ConfirmedAt != nil {
				add(*rw.ConfirmedAt, "rework", "客户确认"+reworkTypeName(rw.Type)+"方案", "", rw.CustomerConfirmedByName)
			}
			if rw.ExecutedAt != nil {
				add(*rw.ExecutedAt, "rework", reworkTypeName(rw.Type)+"执行完成", rw.ResultNote, rw.ExecutedByName)
			}
		}
	}
	for _, cs := range costs {
		add(cs.CreatedAt, "cost", "登记费用 "+f2s(cs.Amount)+" 元", "责任方："+partyName(cs.ResponsibleParty)+"；"+cs.Note, cs.CreatedByName)
	}
	if inst != nil {
		add(inst.InstalledAt, "install", "现场安装完成", inst.Note, inst.InstalledByName)
	}
	if acc != nil {
		add(acc.AcceptedAt, "accept", "客户验收完成", acc.Note, acc.AcceptedByName)
	}
	sort.SliceStable(tl, func(i, j int) bool { return tl[i].At.Before(tl[j].At) })
	return tl
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func f2s(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func reworkTypeName(t string) string {
	if t == "change" {
		return "变更"
	}
	return "返工"
}

func partyName(p string) string {
	switch p {
	case "factory":
		return "工厂"
	case "surveyor":
		return "测量员"
	case "installer":
		return "安装人员"
	case "customer":
		return "客户"
	}
	return p
}

func encodeImages(urls []string) string {
	b, _ := json.Marshal(urls)
	return string(b)
}

func decodeImages(s string) []string {
	var urls []string
	if s == "" {
		return urls
	}
	_ = json.Unmarshal([]byte(s), &urls)
	return urls
}
