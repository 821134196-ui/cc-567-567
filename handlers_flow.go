package main

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func nullTime(t time.Time) *time.Time {
	return &t
}

// ---------- 工厂加工 ----------

func startProduction(c *gin.Context) {
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	u := c.MustGet("user").(*authUser)
	if o.Status != StatusConfirmed {
		c.JSON(http.StatusConflict, gin.H{"error": "客户尚未签字确认尺寸图，工厂不能开工"})
		return
	}
	var v DrawingVersion
	if err := db.First(&v, o.CurrentVersionID).Error; err != nil || v.Status != VerConfirmed {
		c.JSON(http.StatusConflict, gin.H{"error": "找不到客户已确认的图纸版本"})
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	rec := ProductionRecord{
		OrderID: o.ID, VersionID: v.ID, VersionNo: v.VersionNo,
		Status: "in_progress", OperatorID: u.ID, OperatorName: u.Name,
		Note: req.Note, StartedAt: nullTime(time.Now()),
	}
	if err := db.Create(&rec).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	db.Model(&Order{}).Where("id = ?", o.ID).Update("status", StatusProducing)
	c.JSON(http.StatusCreated, rec)
}

// finishProduction 工厂登记实际开孔尺寸并完成加工，同时锁定加工版本
func finishProduction(c *gin.Context) {
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	u := c.MustGet("user").(*authUser)
	if o.Status != StatusProducing {
		c.JSON(http.StatusConflict, gin.H{"error": "工单当前不在加工中"})
		return
	}
	var req struct {
		Actuals []struct {
			Slot  string  `json:"slot"`
			Width float64 `json:"width"`
			Depth float64 `json:"depth"`
			PosX  float64 `json:"pos_x"`
			PosY  float64 `json:"pos_y"`
		} `json:"actuals"`
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	var prod ProductionRecord
	if err := db.Where("order_id = ? AND status = ?", o.ID, "in_progress").
		Order("id desc").First(&prod).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "没有进行中的加工任务"})
		return
	}

	now := time.Now()
	// 登记工厂实际开孔测量记录
	var openings []Opening
	db.Where("version_id = ?", prod.VersionID).Find(&openings)
	actualMap := map[string]struct {
		W, D, X, Y float64
	}{}
	for _, a := range req.Actuals {
		actualMap[a.Slot] = struct{ W, D, X, Y float64 }{a.Width, a.Depth, a.PosX, a.PosY}
	}
	for _, op := range openings {
		a, has := actualMap[op.Slot]
		if !has {
			// 未单独填报时按图纸值视为合格加工
			a = struct{ W, D, X, Y float64 }{op.Width, op.Depth, op.PosX, op.PosY}
		}
		db.Create(&MeasureRecord{
			OrderID: o.ID, VersionID: prod.VersionID, Slot: op.Slot,
			Stage: MeasureFactoryActual, Width: a.W, Depth: a.D, PosX: a.X, PosY: a.Y,
			Note: "工厂加工后实际开孔", MeasuredByID: u.ID, MeasuredByName: u.Name,
		})
	}

	db.Model(&prod).Updates(map[string]any{"status": "done", "finished_at": now, "note": req.Note})
	db.Model(&Order{}).Where("id = ?", o.ID).Updates(map[string]any{
		"status": StatusProduced, "produced_version_id": prod.VersionID,
	})
	c.JSON(http.StatusOK, gin.H{"status": "done", "produced_version_id": prod.VersionID})
}

// ---------- 安装现场 ----------

// submitSiteMeasurement 现场测量：与确认尺寸、实际开孔三方对比，自动判定超差
func submitSiteMeasurement(c *gin.Context) {
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	u := c.MustGet("user").(*authUser)
	if o.Status != StatusProduced && o.Status != StatusReadyInstall && o.Status != StatusBlocked {
		c.JSON(http.StatusConflict, gin.H{"error": "只有已加工完成的台面才能进行安装现场测量"})
		return
	}
	var req struct {
		Slot   string   `json:"slot"`
		Width  float64  `json:"width"`
		Depth  float64  `json:"depth"`
		PosX   float64  `json:"pos_x"`
		PosY   float64  `json:"pos_y"`
		Images []string `json:"images"`
		Note   string   `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Slot == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择开孔并填写现场测量值"})
		return
	}

	// 确认尺寸（客户确认版本）
	var confirmed Opening
	if err := db.Where("version_id = ? AND slot = ?", o.ProducedVersionID, req.Slot).
		First(&confirmed).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "确认图纸中没有该开孔"})
		return
	}
	// 工厂实际开孔（取最近一条）
	var factoryRec MeasureRecord
	db.Where("order_id = ? AND slot = ? AND stage = ?", o.ID, req.Slot, MeasureFactoryActual).
		Order("created_at desc").First(&factoryRec)

	rec := MeasureRecord{
		OrderID: o.ID, VersionID: o.ProducedVersionID, Slot: req.Slot, Stage: MeasureSite,
		Width: req.Width, Depth: req.Depth, PosX: req.PosX, PosY: req.PosY,
		Images: encodeImages(req.Images), Note: req.Note,
		MeasuredByID: u.ID, MeasuredByName: u.Name,
	}
	if err := db.Create(&rec).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	dW := round1(req.Width - confirmed.Width)
	dD := round1(req.Depth - confirmed.Depth)
	dX := round1(req.PosX - confirmed.PosX)
	dY := round1(req.PosY - confirmed.PosY)
	exceeded := math.Abs(dW) > confirmed.Tolerance || math.Abs(dD) > confirmed.Tolerance ||
		math.Abs(dX) > confirmed.Tolerance || math.Abs(dY) > confirmed.Tolerance

	dev := Deviation{
		OrderID: o.ID, Slot: req.Slot, Kind: confirmed.Kind, Tolerance: confirmed.Tolerance,
		ExpectWidth: confirmed.Width, ExpectDepth: confirmed.Depth,
		ExpectPosX: confirmed.PosX, ExpectPosY: confirmed.PosY,
		ActualWidth: req.Width, ActualDepth: req.Depth, ActualPosX: req.PosX, ActualPosY: req.PosY,
		DeltaWidth: dW, DeltaDepth: dD, DeltaPosX: dX, DeltaPosY: dY,
		Exceeded: exceeded, Note: req.Note, FoundByID: u.ID, FoundByName: u.Name, MeasureID: rec.ID,
	}
	if exceeded {
		dev.Status = DevOpen
		db.Model(&Order{}).Where("id = ?", o.ID).Update("status", StatusBlocked)
	} else {
		dev.Status = DevResolved
		dev.ResolvedAt = nullTime(time.Now())
	}
	if err := db.Create(&dev).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 若全部偏差均已闭环，工单回到可安装
	if !exceeded {
		tryMarkReady(o.ID)
	}
	c.JSON(http.StatusCreated, gin.H{"measurement": rec, "deviation": dev})
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func tryMarkReady(orderID uint) {
	var openCnt int64
	db.Model(&Deviation{}).
		Where("order_id = ? AND status IN ?", orderID,
			[]string{DevOpen, DevProposalPending, DevCustomerConfirmed, DevReworkDone}).
		Count(&openCnt)
	if openCnt == 0 {
		db.Model(&Order{}).Where("id = ?", orderID).Update("status", StatusReadyInstall)
	}
}

// proposeRework 安装人员提出返工/变更方案（现场不得擅自改孔）
func proposeRework(c *gin.Context) {
	var dev Deviation
	if err := db.First(&dev, c.Param("did")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "偏差记录不存在"})
		return
	}
	u := c.MustGet("user").(*authUser)
	if !dev.Exceeded {
		c.JSON(http.StatusConflict, gin.H{"error": "该测量项在公差内，无需返工"})
		return
	}
	if dev.Status == DevResolved {
		c.JSON(http.StatusConflict, gin.H{"error": "该偏差已闭环"})
		return
	}
	var req struct {
		Type             string  `json:"type"` // rework / change
		Plan             string  `json:"plan"`
		EstimatedCost    float64 `json:"estimated_cost"`
		ResponsibleParty string  `json:"responsible_party"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Plan == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写返工/变更处理方案"})
		return
	}
	if req.Type != "rework" && req.Type != "change" {
		req.Type = "rework"
	}
	rw := ReworkEvent{
		OrderID: dev.OrderID, DeviationID: dev.ID, Type: req.Type, Plan: req.Plan,
		Status: ReworkProposed, EstimatedCost: req.EstimatedCost,
		ResponsibleParty: req.ResponsibleParty,
		ProposedByID:     u.ID, ProposedByName: u.Name,
	}
	if err := db.Create(&rw).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	db.Model(&dev).Update("status", DevProposalPending)
	c.JSON(http.StatusCreated, rw)
}

// confirmRework 客户确认方案后才能继续
func confirmRework(c *gin.Context) {
	u := c.MustGet("user").(*authUser)
	rw, ok := loadRework(c)
	if !ok {
		return
	}
	if rw.Status != ReworkProposed {
		c.JSON(http.StatusConflict, gin.H{"error": "只有待确认的方案可以确认"})
		return
	}
	db.Model(rw).Updates(map[string]any{
		"status": ReworkConfirmed, "customer_confirmed_by_id": u.ID,
		"customer_confirmed_by_name": u.Name, "confirmed_at": time.Now(),
	})
	db.Model(&Deviation{}).Where("id = ?", rw.DeviationID).Update("status", DevCustomerConfirmed)
	c.JSON(http.StatusOK, gin.H{"status": ReworkConfirmed})
}

func rejectRework(c *gin.Context) {
	rw, ok := loadRework(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	if rw.Status != ReworkProposed {
		c.JSON(http.StatusConflict, gin.H{"error": "只有待确认的方案可以驳回"})
		return
	}
	db.Model(rw).Updates(map[string]any{
		"status": ReworkRejected, "rejected_reason": req.Reason,
	})
	db.Model(&Deviation{}).Where("id = ?", rw.DeviationID).Update("status", DevRejected)
	c.JSON(http.StatusOK, gin.H{"status": ReworkRejected})
}

// executeRework 工厂执行返工：必须有客户确认
func executeRework(c *gin.Context) {
	u := c.MustGet("user").(*authUser)
	rw, ok := loadRework(c)
	if !ok {
		return
	}
	if rw.Status != ReworkConfirmed {
		c.JSON(http.StatusConflict, gin.H{"error": "客户尚未确认返工方案，工厂不能擅自改孔"})
		return
	}
	var req struct {
		ResultNote string `json:"result_note"`
	}
	_ = c.ShouldBindJSON(&req)
	now := time.Now()
	db.Model(rw).Updates(map[string]any{
		"status": ReworkDone, "executed_by_id": u.ID,
		"executed_by_name": u.Name, "executed_at": now, "result_note": req.ResultNote,
	})
	db.Model(&Deviation{}).Where("id = ?", rw.DeviationID).Update("status", DevReworkDone)
	c.JSON(http.StatusOK, gin.H{"status": ReworkDone})
}

// resolveDeviation 返工后由安装人员复核闭环（可附返工后复测记录）
func resolveDeviation(c *gin.Context) {
	u := c.MustGet("user").(*authUser)
	var dev Deviation
	if err := db.First(&dev, c.Param("did")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "偏差记录不存在"})
		return
	}
	if dev.Status != DevReworkDone {
		c.JSON(http.StatusConflict, gin.H{"error": "返工尚未完成，不能闭环"})
		return
	}
	var req struct {
		Width  *float64 `json:"width"`
		Depth  *float64 `json:"depth"`
		PosX   *float64 `json:"pos_x"`
		PosY   *float64 `json:"pos_y"`
		Images []string `json:"images"`
		Note   string   `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Width != nil {
		db.Create(&MeasureRecord{
			OrderID: dev.OrderID, VersionID: 0, Slot: dev.Slot, Stage: MeasurePostRework,
			Width: *req.Width, Depth: deref(req.Depth), PosX: deref(req.PosX), PosY: deref(req.PosY),
			Images: encodeImages(req.Images), Note: req.Note,
			MeasuredByID: u.ID, MeasuredByName: u.Name,
		})
	}
	db.Model(&dev).Updates(map[string]any{"status": DevResolved, "resolved_at": time.Now()})
	tryMarkReady(dev.OrderID)
	c.JSON(http.StatusOK, gin.H{"status": DevResolved})
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func loadRework(c *gin.Context) (*ReworkEvent, bool) {
	var rw ReworkEvent
	if err := db.First(&rw, c.Param("rid")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "返工方案不存在"})
		return nil, false
	}
	return &rw, true
}

// ---------- 安装与验收 ----------

func installOrder(c *gin.Context) {
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	u := c.MustGet("user").(*authUser)
	if o.Status != StatusProduced && o.Status != StatusReadyInstall {
		c.JSON(http.StatusConflict, gin.H{"error": "当前状态不能安装（存在未闭环的开孔偏差时禁止安装）"})
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	rec := InstallationRecord{
		OrderID: o.ID, InstalledByID: u.ID, InstalledByName: u.Name,
		Note: req.Note, InstalledAt: time.Now(),
	}
	if err := db.Create(&rec).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "该工单已登记安装"})
		return
	}
	db.Model(&Order{}).Where("id = ?", o.ID).Update("status", StatusInstalled)
	c.JSON(http.StatusCreated, rec)
}

func acceptOrder(c *gin.Context) {
	u := c.MustGet("user").(*authUser)
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	if o.CustomerID != 0 && o.CustomerID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "只能验收自己的工单"})
		return
	}
	if o.Status != StatusInstalled {
		c.JSON(http.StatusConflict, gin.H{"error": "安装完成后才能验收"})
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	rec := AcceptanceRecord{
		OrderID: o.ID, AcceptedByID: u.ID, AcceptedByName: u.Name,
		Note: req.Note, AcceptedAt: time.Now(),
	}
	if err := db.Create(&rec).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "该工单已验收"})
		return
	}
	db.Model(&Order{}).Where("id = ?", o.ID).Update("status", StatusAccepted)
	c.JSON(http.StatusCreated, rec)
}

// ---------- 费用 ----------

func addCost(c *gin.Context) {
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	u := c.MustGet("user").(*authUser)
	var req struct {
		Amount           float64 `json:"amount"`
		Category         string  `json:"category"`
		ResponsibleParty string  `json:"responsible_party"`
		Note             string  `json:"note"`
		ReworkEventID    uint    `json:"rework_event_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Amount < 0 || req.ResponsibleParty == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写金额与责任方"})
		return
	}
	if req.Category == "" {
		req.Category = "rework"
	}
	rec := CostRecord{
		OrderID: o.ID, ReworkEventID: req.ReworkEventID, Amount: req.Amount,
		Category: req.Category, ResponsibleParty: req.ResponsibleParty, Note: req.Note,
		CreatedByID: u.ID, CreatedByName: u.Name,
	}
	if err := db.Create(&rec).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rec)
}
