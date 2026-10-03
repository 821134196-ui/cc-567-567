package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type openingInput struct {
	Slot      string  `json:"slot"`
	Kind      string  `json:"kind"`
	Model     string  `json:"model"`
	PosX      float64 `json:"pos_x"`
	PosY      float64 `json:"pos_y"`
	Width     float64 `json:"width"`
	Depth     float64 `json:"depth"`
	Tolerance float64 `json:"tolerance"`
}

// createVersion 测量员出新版图纸。已加工台面不会自动套用新尺寸。
func createVersion(c *gin.Context) {
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	u := c.MustGet("user").(*authUser)
	var req struct {
		Note     string         `json:"note"`
		Openings []openingInput `json:"openings"`
		Submit   bool           `json:"submit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Openings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请至少填写一个开孔（水槽/灶具）尺寸"})
		return
	}

	// 已加工台面不能自动套用新尺寸：换版只允许在尚未加工时进行
	if o.ProducedVersionID != 0 || o.Status == StatusProducing {
		c.JSON(http.StatusConflict, gin.H{
			"error": "该工单台面已加工/加工中，图纸换版不能自动套用到已加工台面，请走偏差返工/变更流程",
		})
		return
	}

	var maxNo int
	db.Model(&DrawingVersion{}).Where("order_id = ?", o.ID).Select("COALESCE(MAX(version_no),0)").Scan(&maxNo)

	status := VerDraft
	if req.Submit {
		status = VerSubmitted
	}
	v := DrawingVersion{
		OrderID: o.ID, VersionNo: maxNo + 1, Note: req.Note, Status: status,
		CreatedByID: u.ID, CreatedByName: u.Name,
	}
	if req.Submit {
		v.SubmittedAt = nullTime(time.Now())
	}

	if err := db.Create(&v).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, in := range req.Openings {
		if in.Tolerance <= 0 {
			in.Tolerance = 2
		}
		op := Opening{
			OrderID: o.ID, VersionID: v.ID, Slot: in.Slot, Kind: in.Kind, Model: in.Model,
			PosX: in.PosX, PosY: in.PosY, Width: in.Width, Depth: in.Depth, Tolerance: in.Tolerance,
		}
		if err := db.Create(&op).Error; err != nil {
			return
		}
	}

	// 旧的“已提交待确认”版本作废；已确认版本作为历史基线保留（标记 superseded 仅在客户确认新版时发生）
	db.Model(&DrawingVersion{}).
		Where("order_id = ? AND status = ? AND id <> ?", o.ID, VerSubmitted, v.ID).
		Update("status", VerDraft)

	if req.Submit {
		if o.Status == StatusMeasuring || o.Status == StatusPendingConfirm {
			db.Model(&Order{}).Where("id = ?", o.ID).Update("status", StatusPendingConfirm)
		}
	}

	c.JSON(http.StatusCreated, gin.H{"version_id": v.ID, "version_no": v.VersionNo, "status": v.Status})
}

func getVersion(c *gin.Context, key string) (*DrawingVersion, bool) {
	var v DrawingVersion
	if err := db.First(&v, c.Param(key)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "图纸版本不存在"})
		return nil, false
	}
	return &v, true
}

func submitVersion(c *gin.Context) {
	v, ok := getVersion(c, "vid")
	if !ok {
		return
	}
	if v.Status == VerConfirmed {
		c.JSON(http.StatusConflict, gin.H{"error": "该版本已经客户确认，无需重复提交"})
		return
	}
	if v.Status == VerSuperseded {
		c.JSON(http.StatusConflict, gin.H{"error": "该版本已被替代，不能提交"})
		return
	}
	now := time.Now()
	db.Model(v).Updates(map[string]any{"status": VerSubmitted, "submitted_at": now})
	db.Model(&Order{}).Where("id = ? AND status IN ?", v.OrderID,
		[]string{StatusMeasuring}).Update("status", StatusPendingConfirm)
	c.JSON(http.StatusOK, gin.H{"status": VerSubmitted})
}

// confirmVersion 客户签字确认：只有客户本人可操作；确认后该版本成为加工基线
func confirmVersion(c *gin.Context) {
	u := c.MustGet("user").(*authUser)
	v, ok := getVersion(c, "vid")
	if !ok {
		return
	}
	var o Order
	if err := db.First(&o, v.OrderID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "工单不存在"})
		return
	}
	if o.CustomerID != 0 && o.CustomerID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "只能确认自己工单的图纸"})
		return
	}
	if v.Status != VerSubmitted {
		c.JSON(http.StatusConflict, gin.H{"error": "只有“已提交”的图纸版本可以确认（当前状态：" + v.Status + "）"})
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)

	now := time.Now()
	if err := db.Model(v).Updates(map[string]any{
		"status": VerConfirmed, "confirmed_by_id": u.ID,
		"confirmed_by_name": u.Name, "confirmed_at": now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 同工单其它未确认版本置为 superseded / draft；历史已确认版本标记 superseded
	db.Model(&DrawingVersion{}).
		Where("order_id = ? AND id <> ?", v.OrderID, v.ID).
		Update("status", VerSuperseded)

	db.Create(&Confirmation{
		OrderID: v.OrderID, VersionID: v.ID, VersionNo: v.VersionNo,
		Action: "confirm", SignedName: u.Name, Note: req.Note, CustomerID: u.ID,
	})
	db.Model(&Order{}).Where("id = ?", v.OrderID).Updates(map[string]any{
		"status": StatusConfirmed, "current_version_id": v.ID,
	})
	c.JSON(http.StatusOK, gin.H{"status": VerConfirmed, "version_id": v.ID})
}

func rejectVersion(c *gin.Context) {
	u := c.MustGet("user").(*authUser)
	v, ok := getVersion(c, "vid")
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	if v.Status != VerSubmitted {
		c.JSON(http.StatusConflict, gin.H{"error": "只有已提交的版本可以驳回"})
		return
	}
	db.Model(v).Update("status", VerRejected)
	db.Create(&Confirmation{
		OrderID: v.OrderID, VersionID: v.ID, VersionNo: v.VersionNo,
		Action: "reject", SignedName: u.Name, Note: req.Note, CustomerID: u.ID,
	})
	c.JSON(http.StatusOK, gin.H{"status": VerRejected})
}

// remeasureOpening 测量员对同一开孔复测：保留前后数值及原因
func remeasureOpening(c *gin.Context) {
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	u := c.MustGet("user").(*authUser)
	slot := c.Param("slot")
	var req struct {
		Width  float64  `json:"width"`
		Depth  float64  `json:"depth"`
		PosX   float64  `json:"pos_x"`
		PosY   float64  `json:"pos_y"`
		Reason string   `json:"reason"`
		Images []string `json:"images"`
		Note   string   `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if req.Reason == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "复测必须填写原因"})
		return
	}

	// 找到该开孔最近一次测量记录作为“复测前数值”
	var prev MeasureRecord
	prevErr := db.Where("order_id = ? AND slot = ? AND stage IN ?", o.ID, slot,
		[]string{MeasureInitial, MeasureRemeasure}).Order("created_at desc").First(&prev).Error
	versionID := o.CurrentVersionID
	if prevErr == nil {
		versionID = prev.VersionID
	}

	rec := MeasureRecord{
		OrderID: o.ID, VersionID: versionID, Slot: slot, Stage: MeasureRemeasure,
		Width: req.Width, Depth: req.Depth, PosX: req.PosX, PosY: req.PosY,
		Reason: req.Reason, Images: encodeImages(req.Images), Note: req.Note,
		MeasuredByID: u.ID, MeasuredByName: u.Name,
	}
	if prevErr == nil {
		rec.PrevWidth = prev.Width
		rec.PrevDepth = prev.Depth
		rec.PrevPosX = prev.PosX
		rec.PrevPosY = prev.PosY
	}
	if err := db.Create(&rec).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rec)
}
