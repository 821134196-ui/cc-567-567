package main

import (
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func audit(orderID uint, actor, action, detail string) {
	db.Create(&AuditLog{OrderID: orderID, Actor: actor, Action: action, Detail: detail})
}

func kindLabel(kind string) string {
	switch kind {
	case "sink":
		return "水槽"
	case "cooktop":
		return "灶具"
	}
	return kind
}

func cutoutLabel(kind, model string) string {
	return strings.TrimSpace(kindLabel(kind) + " " + model)
}

func fail(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg})
}

// ---------- 查询 ----------

func listUsers(c *gin.Context) {
	var users []User
	db.Order("id").Find(&users)
	out := []gin.H{}
	for _, u := range users {
		out = append(out, gin.H{"id": u.ID, "username": u.Username, "name": u.Name, "role": u.Role})
	}
	c.JSON(http.StatusOK, out)
}

func meHandler(c *gin.Context) {
	c.JSON(http.StatusOK, currentUser(c))
}

func listOrders(c *gin.Context) {
	var orders []Order
	db.Order("id").Find(&orders)
	views := []gin.H{}
	for _, o := range orders {
		v := gin.H{
			"id": o.ID, "orderNo": o.OrderNo, "customerName": o.CustomerName,
			"address": o.Address, "status": o.Status, "createdAt": o.CreatedAt,
		}
		if o.CurrentDrawingID != nil {
			var d Drawing
			if err := db.First(&d, *o.CurrentDrawingID).Error; err == nil {
				v["currentVersion"] = d.Version
			}
		}
		var fab Fabrication
		if err := db.Where("order_id = ?", o.ID).First(&fab).Error; err == nil {
			v["fabricatedVersion"] = fab.DrawingVersion
		}
		var cu User
		if err := db.First(&cu, o.CustomerUserID).Error; err == nil {
			v["customerAccount"] = cu.Username
		}
		views = append(views, v)
	}
	c.JSON(http.StatusOK, views)
}

type dimSet struct {
	PosX   float64 `json:"posX"`
	PosY   float64 `json:"posY"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type comparisonRow struct {
	CutoutID  uint    `json:"cutoutId"`
	Label     string  `json:"label"`
	Confirmed dimSet  `json:"confirmed"`
	Actual    *dimSet `json:"actual"`
	Field     *dimSet `json:"field"`
	ActualDev *dimSet `json:"actualDev"`
	FieldDev  *dimSet `json:"fieldDev"`
	Exceeded  bool    `json:"exceeded"`
}

func devExceeded(d dimSet) bool {
	return math.Abs(d.PosX) > ToleranceMM || math.Abs(d.PosY) > ToleranceMM ||
		math.Abs(d.Width) > ToleranceMM || math.Abs(d.Height) > ToleranceMM
}

// 基准图纸：已加工则锁定加工版本，否则用当前生效版本
func baselineDrawing(order *Order, drawings []Drawing, fab *Fabrication) *Drawing {
	if fab != nil {
		for i := range drawings {
			if drawings[i].ID == fab.DrawingID {
				return &drawings[i]
			}
		}
	}
	if order.CurrentDrawingID != nil {
		for i := range drawings {
			if drawings[i].ID == *order.CurrentDrawingID {
				return &drawings[i]
			}
		}
	}
	for i := len(drawings) - 1; i >= 0; i-- {
		if drawings[i].Status == DrawingConfirmed {
			return &drawings[i]
		}
	}
	if len(drawings) > 0 {
		return &drawings[len(drawings)-1]
	}
	return nil
}

func buildComparison(order *Order, drawings []Drawing, fab *Fabrication, fields []FieldMeasurement) gin.H {
	base := baselineDrawing(order, drawings, fab)
	actualByCutout := map[uint]ActualCutout{}
	if fab != nil {
		for _, a := range fab.Actuals {
			actualByCutout[a.CutoutID] = a
		}
	}
	latestField := map[uint]FieldMeasurement{}
	for _, f := range fields { // 按 id 升序，后写覆盖 → 取最新一次
		latestField[f.CutoutID] = f
	}

	rows := []comparisonRow{}
	hasDeviation := false
	if base != nil {
		for i := range base.Cutouts {
			ct := &base.Cutouts[i]
			row := comparisonRow{
				CutoutID:  ct.ID,
				Label:     cutoutLabel(ct.Kind, ct.Model),
				Confirmed: dimSet{ct.PosX, ct.PosY, ct.Width, ct.Height},
			}
			if a, ok := actualByCutout[ct.ID]; ok {
				d := dimSet{a.PosX, a.PosY, a.Width, a.Height}
				row.Actual = &d
				dev := dimSet{a.PosX - ct.PosX, a.PosY - ct.PosY, a.Width - ct.Width, a.Height - ct.Height}
				row.ActualDev = &dev
				if devExceeded(dev) {
					row.Exceeded = true
				}
			}
			if f, ok := latestField[ct.ID]; ok {
				d := dimSet{f.PosX, f.PosY, f.Width, f.Height}
				row.Field = &d
				dev := dimSet{f.PosX - ct.PosX, f.PosY - ct.PosY, f.Width - ct.Width, f.Height - ct.Height}
				row.FieldDev = &dev
				if devExceeded(dev) {
					row.Exceeded = true
				}
			}
			if row.Exceeded {
				hasDeviation = true
			}
			rows = append(rows, row)
		}
	}

	// 超差是否仍未解决：存在超差开孔但没有已完成的方案覆盖
	unresolved := false
	for _, r := range rows {
		if !r.Exceeded {
			continue
		}
		var cnt int64
		db.Model(&ReworkPlan{}).Where("order_id = ? AND cutout_id = ? AND status = ?", order.ID, r.CutoutID, PlanCompleted).Count(&cnt)
		if cnt == 0 {
			unresolved = true
		}
	}

	baseVersion := 0
	if base != nil {
		baseVersion = base.Version
	}
	return gin.H{
		"baselineVersion":     baseVersion,
		"rows":                rows,
		"hasDeviation":        hasDeviation,
		"unresolvedDeviation": unresolved,
		"tolerance":           ToleranceMM,
	}
}

func getOrder(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var order Order
	if err := db.First(&order, id).Error; err != nil {
		fail(c, http.StatusNotFound, "订单不存在")
		return
	}
	var drawings []Drawing
	db.Preload("Cutouts.Images").Where("order_id = ?", order.ID).Order("version").Find(&drawings)
	var confirmations []Confirmation
	db.Where("order_id = ?", order.ID).Order("id").Find(&confirmations)
	var fabrication Fabrication
	var fabPtr *Fabrication
	if err := db.Preload("Actuals").Where("order_id = ?", order.ID).First(&fabrication).Error; err == nil {
		fabPtr = &fabrication
	}
	var fields []FieldMeasurement
	db.Where("order_id = ?", order.ID).Order("id").Find(&fields)
	var remeasures []Remeasure
	db.Where("order_id = ?", order.ID).Order("id").Find(&remeasures)
	var plans []ReworkPlan
	db.Where("order_id = ?", order.ID).Order("id").Find(&plans)
	var logs []AuditLog
	db.Where("order_id = ?", order.ID).Order("id").Find(&logs)

	c.JSON(http.StatusOK, gin.H{
		"order":            order,
		"drawings":         drawings,
		"confirmations":    confirmations,
		"fabrication":      fabPtr,
		"fieldMeasurement": fields,
		"remeasures":       remeasures,
		"reworkPlans":      plans,
		"auditLogs":        logs,
		"comparison":       buildComparison(&order, drawings, fabPtr, fields),
	})
}

// ---------- 测量员 ----------

type cutoutInput struct {
	Kind   string  `json:"kind"`
	Model  string  `json:"model"`
	PosX   float64 `json:"posX"`
	PosY   float64 `json:"posY"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func validCutout(in cutoutInput) string {
	if in.Kind != "sink" && in.Kind != "cooktop" {
		return "开孔类型必须是 sink(水槽) 或 cooktop(灶具)"
	}
	if strings.TrimSpace(in.Model) == "" {
		return "请填写型号"
	}
	if in.Width <= 0 || in.Height <= 0 {
		return "开孔尺寸必须大于 0"
	}
	return ""
}

func createOrder(c *gin.Context) {
	u := currentUser(c)
	var req struct {
		CustomerName   string        `json:"customerName"`
		CustomerUserID uint          `json:"customerUserId"`
		Address        string        `json:"address"`
		Cutouts        []cutoutInput `json:"cutouts"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if strings.TrimSpace(req.CustomerName) == "" {
		fail(c, http.StatusBadRequest, "请填写客户姓名")
		return
	}
	var customer User
	if err := db.First(&customer, req.CustomerUserID).Error; err != nil || customer.Role != RoleCustomer {
		fail(c, http.StatusBadRequest, "请选择客户账号")
		return
	}
	if len(req.Cutouts) == 0 {
		fail(c, http.StatusBadRequest, "至少录入一个开孔")
		return
	}
	for _, ct := range req.Cutouts {
		if msg := validCutout(ct); msg != "" {
			fail(c, http.StatusBadRequest, msg)
			return
		}
	}

	var count int64
	db.Model(&Order{}).Count(&count)
	order := Order{
		OrderNo:        fmt.Sprintf("ORD-2026-%03d", count+1),
		CustomerName:   req.CustomerName,
		CustomerUserID: customer.ID,
		Address:        req.Address,
		Status:         OrderMeasuring,
	}
	if err := db.Create(&order).Error; err != nil {
		fail(c, http.StatusInternalServerError, "创建订单失败")
		return
	}
	drawing := Drawing{OrderID: order.ID, Version: 1, Status: DrawingDraft, CreatedBy: u.Name, Note: "初测"}
	db.Create(&drawing)
	for _, ct := range req.Cutouts {
		db.Create(&Cutout{
			DrawingID: drawing.ID, Kind: ct.Kind, Model: ct.Model,
			PosX: ct.PosX, PosY: ct.PosY, Width: ct.Width, Height: ct.Height,
		})
	}
	audit(order.ID, u.Name, "创建订单", fmt.Sprintf("创建订单 %s 并录入初测数据（图纸 v1 草稿）", order.OrderNo))
	c.JSON(http.StatusOK, gin.H{"id": order.ID, "orderNo": order.OrderNo})
}

func createDrawing(c *gin.Context) {
	u := currentUser(c)
	orderID, _ := strconv.Atoi(c.Param("id"))
	var order Order
	if err := db.First(&order, orderID).Error; err != nil {
		fail(c, http.StatusNotFound, "订单不存在")
		return
	}
	if order.Status == OrderCompleted {
		fail(c, http.StatusConflict, "订单已完成，不能再换版")
		return
	}
	var draftCnt int64
	db.Model(&Drawing{}).Where("order_id = ? AND status = ?", order.ID, DrawingDraft).Count(&draftCnt)
	if draftCnt > 0 {
		fail(c, http.StatusConflict, "已存在未提交的草稿版本，请先处理")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)

	var latest Drawing
	maxVer := 0
	if err := db.Preload("Cutouts").Where("order_id = ?", order.ID).Order("version DESC").First(&latest).Error; err == nil {
		maxVer = latest.Version
	}
	drawing := Drawing{
		OrderID: order.ID, Version: maxVer + 1, Status: DrawingDraft,
		CreatedBy: u.Name, Note: req.Note,
	}
	db.Create(&drawing)
	// 换版默认基于上一版复制开孔，测量员在此基础上修改
	for _, ct := range latest.Cutouts {
		db.Create(&Cutout{
			DrawingID: drawing.ID, Kind: ct.Kind, Model: ct.Model,
			PosX: ct.PosX, PosY: ct.PosY, Width: ct.Width, Height: ct.Height,
		})
	}
	audit(order.ID, u.Name, "图纸换版", fmt.Sprintf("创建图纸 v%d 草稿（基于 v%d，原因：%s）", drawing.Version, maxVer, req.Note))
	c.JSON(http.StatusOK, gin.H{"id": drawing.ID, "version": drawing.Version})
}

func loadDraftDrawing(c *gin.Context, drawingID int) (*Drawing, bool) {
	var d Drawing
	if err := db.First(&d, drawingID).Error; err != nil {
		fail(c, http.StatusNotFound, "图纸不存在")
		return nil, false
	}
	if d.Status != DrawingDraft {
		fail(c, http.StatusConflict, "只有草稿状态的图纸可以修改")
		return nil, false
	}
	return &d, true
}

func addCutout(c *gin.Context) {
	u := currentUser(c)
	drawingID, _ := strconv.Atoi(c.Param("id"))
	d, ok := loadDraftDrawing(c, drawingID)
	if !ok {
		return
	}
	var in cutoutInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if msg := validCutout(in); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	ct := Cutout{DrawingID: d.ID, Kind: in.Kind, Model: in.Model, PosX: in.PosX, PosY: in.PosY, Width: in.Width, Height: in.Height}
	db.Create(&ct)
	audit(d.OrderID, u.Name, "新增开孔", fmt.Sprintf("图纸 v%d 新增开孔 %s", d.Version, cutoutLabel(in.Kind, in.Model)))
	c.JSON(http.StatusOK, ct)
}

// updateCutout 重新测量：保留前后数值与原因
func updateCutout(c *gin.Context) {
	u := currentUser(c)
	cutoutID, _ := strconv.Atoi(c.Param("id"))
	var ct Cutout
	if err := db.First(&ct, cutoutID).Error; err != nil {
		fail(c, http.StatusNotFound, "开孔不存在")
		return
	}
	d, ok := loadDraftDrawing(c, int(ct.DrawingID))
	if !ok {
		return
	}
	var req struct {
		Model  string  `json:"model"`
		PosX   float64 `json:"posX"`
		PosY   float64 `json:"posY"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
		Reason string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		fail(c, http.StatusBadRequest, "重新测量必须填写原因")
		return
	}
	if req.Width <= 0 || req.Height <= 0 {
		fail(c, http.StatusBadRequest, "开孔尺寸必须大于 0")
		return
	}
	model := ct.Model
	if strings.TrimSpace(req.Model) != "" {
		model = req.Model
	}
	db.Create(&Remeasure{
		OrderID: d.OrderID, CutoutID: ct.ID, Label: cutoutLabel(ct.Kind, model),
		OldPosX: ct.PosX, OldPosY: ct.PosY, OldWidth: ct.Width, OldHeight: ct.Height,
		NewPosX: req.PosX, NewPosY: req.PosY, NewWidth: req.Width, NewHeight: req.Height,
		Reason: req.Reason, CreatedBy: u.Name,
	})
	ct.Model = model
	ct.PosX, ct.PosY, ct.Width, ct.Height = req.PosX, req.PosY, req.Width, req.Height
	db.Save(&ct)
	audit(d.OrderID, u.Name, "重新测量", fmt.Sprintf("开孔 %s 复测（原因：%s）", cutoutLabel(ct.Kind, ct.Model), req.Reason))
	c.JSON(http.StatusOK, ct)
}

func deleteCutout(c *gin.Context) {
	u := currentUser(c)
	cutoutID, _ := strconv.Atoi(c.Param("id"))
	var ct Cutout
	if err := db.First(&ct, cutoutID).Error; err != nil {
		fail(c, http.StatusNotFound, "开孔不存在")
		return
	}
	d, ok := loadDraftDrawing(c, int(ct.DrawingID))
	if !ok {
		return
	}
	db.Where("cutout_id = ?", ct.ID).Delete(&MeasurementImage{})
	db.Delete(&ct)
	audit(d.OrderID, u.Name, "删除开孔", fmt.Sprintf("图纸 v%d 删除开孔 %s", d.Version, cutoutLabel(ct.Kind, ct.Model)))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func uploadImage(c *gin.Context) {
	u := currentUser(c)
	cutoutID, _ := strconv.Atoi(c.Param("id"))
	var ct Cutout
	if err := db.First(&ct, cutoutID).Error; err != nil {
		fail(c, http.StatusNotFound, "开孔不存在")
		return
	}
	var d Drawing
	if err := db.First(&d, ct.DrawingID).Error; err != nil {
		fail(c, http.StatusNotFound, "图纸不存在")
		return
	}
	if d.Status != DrawingDraft && d.Status != DrawingPending {
		fail(c, http.StatusConflict, "图纸已确认，不能再上传测量图片")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		fail(c, http.StatusBadRequest, "请选择图片文件")
		return
	}
	if file.Size > 5<<20 {
		fail(c, http.StatusBadRequest, "图片不能超过 5MB")
		return
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	if !allowed[ext] {
		fail(c, http.StatusBadRequest, "仅支持 jpg/png/gif/webp 图片")
		return
	}
	name := fmt.Sprintf("cutout_%d_%d%s", ct.ID, time.Now().UnixNano(), ext)
	path := filepath.Join("uploads", name)
	if err := c.SaveUploadedFile(file, path); err != nil {
		fail(c, http.StatusInternalServerError, "保存图片失败")
		return
	}
	img := MeasurementImage{CutoutID: ct.ID, FilePath: "/uploads/" + name, UploadedBy: u.Name}
	db.Create(&img)
	audit(d.OrderID, u.Name, "上传测量图片", fmt.Sprintf("开孔 %s 上传图片", cutoutLabel(ct.Kind, ct.Model)))
	c.JSON(http.StatusOK, img)
}

func submitDrawing(c *gin.Context) {
	u := currentUser(c)
	drawingID, _ := strconv.Atoi(c.Param("id"))
	d, ok := loadDraftDrawing(c, drawingID)
	if !ok {
		return
	}
	var cnt int64
	db.Model(&Cutout{}).Where("drawing_id = ?", d.ID).Count(&cnt)
	if cnt == 0 {
		fail(c, http.StatusConflict, "图纸还没有开孔数据，不能提交")
		return
	}
	d.Status = DrawingPending
	db.Save(d)
	db.Model(&Order{}).Where("id = ?", d.OrderID).Update("status", OrderPendingConfirm)
	audit(d.OrderID, u.Name, "提交确认", fmt.Sprintf("图纸 v%d 提交客户确认", d.Version))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---------- 客户 ----------

func confirmDrawing(c *gin.Context) {
	u := currentUser(c)
	drawingID, _ := strconv.Atoi(c.Param("id"))
	var d Drawing
	if err := db.First(&d, drawingID).Error; err != nil {
		fail(c, http.StatusNotFound, "图纸不存在")
		return
	}
	if d.Status != DrawingPending {
		fail(c, http.StatusConflict, "该图纸不在待确认状态")
		return
	}
	var order Order
	if err := db.First(&order, d.OrderID).Error; err != nil {
		fail(c, http.StatusNotFound, "订单不存在")
		return
	}
	if order.CustomerUserID != u.ID {
		fail(c, http.StatusForbidden, "只能确认本人订单的图纸")
		return
	}
	var req struct {
		Signature string `json:"signature"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Signature) == "" {
		fail(c, http.StatusBadRequest, "请填写签名")
		return
	}

	now := time.Now()
	d.Status = DrawingConfirmed
	d.ConfirmedBy = u.Name
	d.ConfirmedAt = &now
	db.Save(d)
	// 旧确认版作废
	db.Model(&Drawing{}).Where("order_id = ? AND status = ? AND id <> ?", order.ID, DrawingConfirmed, d.ID).
		Update("status", DrawingSuperseded)
	db.Create(&Confirmation{
		OrderID: order.ID, DrawingID: d.ID, Version: d.Version,
		Customer: u.Name, Signature: req.Signature,
	})

	// 关键规则：已加工的台面不自动套用新尺寸
	var fabCnt int64
	db.Model(&Fabrication{}).Where("order_id = ?", order.ID).Count(&fabCnt)
	if fabCnt == 0 {
		db.Model(&Order{}).Where("id = ?", order.ID).Updates(map[string]interface{}{
			"current_drawing_id": d.ID, "status": OrderConfirmed,
		})
		audit(order.ID, u.Name, "客户确认图纸", fmt.Sprintf("客户签字确认图纸 v%d，订单进入待加工", d.Version))
	} else {
		audit(order.ID, u.Name, "客户确认图纸", fmt.Sprintf("客户签字确认图纸 v%d；台面已按旧版加工，新尺寸不自动套用", d.Version))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "applied": fabCnt == 0})
}

// ---------- 工厂 ----------

func fabricateOrder(c *gin.Context) {
	u := currentUser(c)
	orderID, _ := strconv.Atoi(c.Param("id"))
	var order Order
	if err := db.First(&order, orderID).Error; err != nil {
		fail(c, http.StatusNotFound, "订单不存在")
		return
	}
	if order.Status == OrderPendingConfirm || order.Status == OrderMeasuring {
		fail(c, http.StatusConflict, "客户尚未确认图纸，不能加工")
		return
	}
	if order.Status != OrderConfirmed || order.CurrentDrawingID == nil {
		fail(c, http.StatusConflict, "订单当前状态不能加工")
		return
	}
	var drawing Drawing
	if err := db.Preload("Cutouts").First(&drawing, *order.CurrentDrawingID).Error; err != nil {
		fail(c, http.StatusConflict, "没有已确认的图纸版本，不能加工")
		return
	}
	var req struct {
		Note    string `json:"note"`
		Actuals []struct {
			CutoutID uint    `json:"cutoutId"`
			PosX     float64 `json:"posX"`
			PosY     float64 `json:"posY"`
			Width    float64 `json:"width"`
			Height   float64 `json:"height"`
		} `json:"actuals"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	actualByCutout := map[uint]struct {
		PosX   float64 `json:"posX"`
		PosY   float64 `json:"posY"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}{}
	for _, a := range req.Actuals {
		actualByCutout[a.CutoutID] = struct {
			PosX   float64 `json:"posX"`
			PosY   float64 `json:"posY"`
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		}{a.PosX, a.PosY, a.Width, a.Height}
	}
	for _, ct := range drawing.Cutouts {
		if _, ok := actualByCutout[ct.ID]; !ok {
			fail(c, http.StatusBadRequest, fmt.Sprintf("请填写开孔 %s 的实际尺寸", cutoutLabel(ct.Kind, ct.Model)))
			return
		}
	}

	fab := Fabrication{
		OrderID: order.ID, DrawingID: drawing.ID, DrawingVersion: drawing.Version,
		ProcessedBy: u.Name, Note: req.Note,
	}
	db.Create(&fab)
	for _, ct := range drawing.Cutouts {
		a := actualByCutout[ct.ID]
		db.Create(&ActualCutout{
			FabricationID: fab.ID, CutoutID: ct.ID, Label: cutoutLabel(ct.Kind, ct.Model),
			PosX: a.PosX, PosY: a.PosY, Width: a.Width, Height: a.Height,
		})
	}
	db.Model(&Order{}).Where("id = ?", order.ID).Update("status", OrderProcessed)
	audit(order.ID, u.Name, "加工完成", fmt.Sprintf("按图纸 v%d 完成加工，该版本尺寸已锁定", drawing.Version))
	c.JSON(http.StatusOK, gin.H{"id": fab.ID, "drawingVersion": drawing.Version})
}

// ---------- 安装员 ----------

func baselineCutout(order *Order, cutoutID uint) (*Cutout, *Drawing, error) {
	var drawings []Drawing
	db.Preload("Cutouts").Where("order_id = ?", order.ID).Order("version").Find(&drawings)
	var fab Fabrication
	var fabPtr *Fabrication
	if err := db.Preload("Actuals").Where("order_id = ?", order.ID).First(&fab).Error; err == nil {
		fabPtr = &fab
	}
	base := baselineDrawing(order, drawings, fabPtr)
	if base == nil {
		return nil, nil, fmt.Errorf("没有基准图纸")
	}
	for i := range base.Cutouts {
		if base.Cutouts[i].ID == cutoutID {
			return &base.Cutouts[i], base, nil
		}
	}
	return nil, base, fmt.Errorf("开孔不属于基准图纸")
}

func addFieldMeasurement(c *gin.Context) {
	u := currentUser(c)
	orderID, _ := strconv.Atoi(c.Param("id"))
	var order Order
	if err := db.First(&order, orderID).Error; err != nil {
		fail(c, http.StatusNotFound, "订单不存在")
		return
	}
	if order.Status != OrderProcessed && order.Status != OrderReworkPending {
		fail(c, http.StatusConflict, "订单未加工或已完成，不能录入现场测量")
		return
	}
	var req struct {
		CutoutID uint    `json:"cutoutId"`
		PosX     float64 `json:"posX"`
		PosY     float64 `json:"posY"`
		Width    float64 `json:"width"`
		Height   float64 `json:"height"`
		Note     string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	ct, _, err := baselineCutout(&order, req.CutoutID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	fm := FieldMeasurement{
		OrderID: order.ID, CutoutID: ct.ID, Label: cutoutLabel(ct.Kind, ct.Model),
		PosX: req.PosX, PosY: req.PosY, Width: req.Width, Height: req.Height,
		Note: req.Note, MeasuredBy: u.Name,
	}
	db.Create(&fm)
	dev := dimSet{req.PosX - ct.PosX, req.PosY - ct.PosY, req.Width - ct.Width, req.Height - ct.Height}
	exceeded := devExceeded(dev)
	audit(order.ID, u.Name, "现场测量", fmt.Sprintf("开孔 %s 现场复测 %.0f×%.0f（%s）",
		fm.Label, req.Width, req.Height, map[bool]string{true: "超差", false: "在公差内"}[exceeded]))
	c.JSON(http.StatusOK, gin.H{"id": fm.ID, "deviation": dev, "exceeded": exceeded})
}

func proposePlan(c *gin.Context) {
	u := currentUser(c)
	orderID, _ := strconv.Atoi(c.Param("id"))
	var order Order
	if err := db.First(&order, orderID).Error; err != nil {
		fail(c, http.StatusNotFound, "订单不存在")
		return
	}
	if order.Status != OrderProcessed && order.Status != OrderReworkPending {
		fail(c, http.StatusConflict, "当前状态不能提出返工/变更方案")
		return
	}
	var req struct {
		CutoutID         uint    `json:"cutoutId"`
		Type             string  `json:"type"`
		Description      string  `json:"description"`
		Cost             float64 `json:"cost"`
		ResponsibleParty string  `json:"responsibleParty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.Type != "rework" && req.Type != "change" {
		fail(c, http.StatusBadRequest, "方案类型必须是 rework(返工) 或 change(变更)")
		return
	}
	partyOK := map[string]bool{RoleFactory: true, RoleSurveyor: true, RoleCustomer: true, RoleInstaller: true}
	if !partyOK[req.ResponsibleParty] {
		fail(c, http.StatusBadRequest, "责任方不合法")
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		fail(c, http.StatusBadRequest, "请填写方案说明")
		return
	}
	if req.Cost < 0 {
		fail(c, http.StatusBadRequest, "费用不能为负")
		return
	}
	ct, _, err := baselineCutout(&order, req.CutoutID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	plan := ReworkPlan{
		OrderID: order.ID, CutoutID: ct.ID, CutoutLabel: cutoutLabel(ct.Kind, ct.Model),
		Type: req.Type, Description: req.Description, Cost: req.Cost,
		ResponsibleParty: req.ResponsibleParty, Status: PlanPending, ProposedBy: u.Name,
	}
	db.Create(&plan)
	db.Model(&Order{}).Where("id = ?", order.ID).Update("status", OrderReworkPending)
	typeLabel := map[string]string{"rework": "返工", "change": "变更"}[req.Type]
	audit(order.ID, u.Name, "提出方案", fmt.Sprintf("针对开孔 %s 提出%s方案，费用 %.0f 元，待客户确认", plan.CutoutLabel, typeLabel, req.Cost))
	c.JSON(http.StatusOK, plan)
}

// ---------- 客户：方案审批 ----------

func decidePlan(c *gin.Context) {
	u := currentUser(c)
	planID, _ := strconv.Atoi(c.Param("id"))
	var plan ReworkPlan
	if err := db.First(&plan, planID).Error; err != nil {
		fail(c, http.StatusNotFound, "方案不存在")
		return
	}
	var order Order
	if err := db.First(&order, plan.OrderID).Error; err != nil {
		fail(c, http.StatusNotFound, "订单不存在")
		return
	}
	if order.CustomerUserID != u.ID {
		fail(c, http.StatusForbidden, "只能审批本人订单的方案")
		return
	}
	if plan.Status != PlanPending {
		fail(c, http.StatusConflict, "该方案已处理")
		return
	}
	var req struct {
		Approve bool `json:"approve"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	now := time.Now()
	plan.DecidedBy = u.Name
	plan.DecidedAt = &now
	if req.Approve {
		plan.Status = PlanApproved
	} else {
		plan.Status = PlanRejected
	}
	db.Save(&plan)
	// 若没有其他待处理方案，订单回到已加工状态
	if !req.Approve {
		var openCnt int64
		db.Model(&ReworkPlan{}).Where("order_id = ? AND status IN ?", order.ID, []string{PlanPending, PlanApproved}).Count(&openCnt)
		if openCnt == 0 {
			db.Model(&Order{}).Where("id = ?", order.ID).Update("status", OrderProcessed)
		}
	}
	word := "拒绝"
	if req.Approve {
		word = "批准"
	}
	audit(order.ID, u.Name, "客户审批方案", fmt.Sprintf("客户%s了针对开孔 %s 的方案", word, plan.CutoutLabel))
	c.JSON(http.StatusOK, plan)
}

// ---------- 安装员：返工完成 / 竣工验收 ----------

func completePlan(c *gin.Context) {
	u := currentUser(c)
	planID, _ := strconv.Atoi(c.Param("id"))
	var plan ReworkPlan
	if err := db.First(&plan, planID).Error; err != nil {
		fail(c, http.StatusNotFound, "方案不存在")
		return
	}
	if plan.Status != PlanApproved {
		fail(c, http.StatusConflict, "方案需经客户批准后才能执行完成")
		return
	}
	now := time.Now()
	plan.Status = PlanCompleted
	plan.CompletedBy = u.Name
	plan.CompletedAt = &now
	db.Save(&plan)
	var openCnt int64
	db.Model(&ReworkPlan{}).Where("order_id = ? AND status IN ?", plan.OrderID, []string{PlanPending, PlanApproved}).Count(&openCnt)
	if openCnt == 0 {
		db.Model(&Order{}).Where("id = ?", plan.OrderID).Update("status", OrderProcessed)
	}
	audit(plan.OrderID, u.Name, "返工完成", fmt.Sprintf("开孔 %s 的方案已执行完成", plan.CutoutLabel))
	c.JSON(http.StatusOK, plan)
}

func completeOrder(c *gin.Context) {
	u := currentUser(c)
	orderID, _ := strconv.Atoi(c.Param("id"))
	var order Order
	if err := db.First(&order, orderID).Error; err != nil {
		fail(c, http.StatusNotFound, "订单不存在")
		return
	}
	if order.Status != OrderProcessed && order.Status != OrderReworkPending {
		fail(c, http.StatusConflict, "订单当前状态不能验收")
		return
	}
	var openCnt int64
	db.Model(&ReworkPlan{}).Where("order_id = ? AND status IN ?", order.ID, []string{PlanPending, PlanApproved}).Count(&openCnt)
	if openCnt > 0 {
		fail(c, http.StatusConflict, "存在未完成的返工/变更方案，不能验收")
		return
	}
	var drawings []Drawing
	db.Preload("Cutouts").Where("order_id = ?", order.ID).Order("version").Find(&drawings)
	var fab Fabrication
	var fabPtr *Fabrication
	if err := db.Preload("Actuals").Where("order_id = ?", order.ID).First(&fab).Error; err == nil {
		fabPtr = &fab
	}
	var fields []FieldMeasurement
	db.Where("order_id = ?", order.ID).Order("id").Find(&fields)
	cmp := buildComparison(&order, drawings, fabPtr, fields)
	if unresolved, _ := cmp["unresolvedDeviation"].(bool); unresolved {
		fail(c, http.StatusConflict, "存在超差开孔尚未处理：请先提出返工/变更方案并经客户确认、执行完成")
		return
	}
	db.Model(&Order{}).Where("id = ?", order.ID).Update("status", OrderCompleted)
	audit(order.ID, u.Name, "安装验收", "安装验收完成，订单关闭")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
