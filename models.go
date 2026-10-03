package main

import "time"

// 岗位角色
const (
	RoleSurveyor  = "surveyor"  // 测量员
	RoleFactory   = "factory"   // 工厂
	RoleInstaller = "installer" // 安装员
	RoleCustomer  = "customer"  // 客户
)

// 订单状态
const (
	OrderMeasuring      = "measuring"       // 测量中（图纸草稿）
	OrderPendingConfirm = "pending_confirm" // 待客户确认
	OrderConfirmed      = "confirmed"       // 已确认待加工
	OrderProcessed      = "processed"       // 已加工待安装
	OrderReworkPending  = "rework_pending"  // 返工/变更方案待处理
	OrderCompleted      = "completed"       // 安装验收完成
)

// 图纸版本状态
const (
	DrawingDraft      = "draft"      // 草稿（测量员可改）
	DrawingPending    = "pending"    // 已提交待客户确认
	DrawingConfirmed  = "confirmed"  // 客户已确认
	DrawingSuperseded = "superseded" // 被更新版本取代
)

// 返工/变更方案状态
const (
	PlanPending   = "pending"
	PlanApproved  = "approved"
	PlanRejected  = "rejected"
	PlanCompleted = "completed"
)

// 尺寸公差（mm），任一项偏差绝对值超过即判为超差
const ToleranceMM = 2.0

type User struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Username string `gorm:"uniqueIndex;size:64" json:"username"`
	Password string `json:"-"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

type Order struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	OrderNo          string    `gorm:"uniqueIndex;size:32" json:"orderNo"`
	CustomerName     string    `json:"customerName"`
	CustomerUserID   uint      `json:"customerUserId"` // 客户账号，只有本人能确认图纸/方案
	Address          string    `json:"address"`
	Status           string    `json:"status"`
	CurrentDrawingID *uint     `json:"currentDrawingId"` // 当前生效图纸；已加工后不再随新版本变化
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Drawing struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	OrderID     uint       `gorm:"index" json:"orderId"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
	Note        string     `json:"note"`
	CreatedBy   string     `json:"createdBy"`
	ConfirmedBy string     `json:"confirmedBy"`
	ConfirmedAt *time.Time `json:"confirmedAt"`
	Cutouts     []Cutout   `gorm:"foreignKey:DrawingID" json:"cutouts"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// 开孔：水槽或灶具
type Cutout struct {
	ID        uint               `gorm:"primaryKey" json:"id"`
	DrawingID uint               `gorm:"index" json:"drawingId"`
	Kind      string             `json:"kind"` // sink 水槽 / cooktop 灶具
	Model     string             `json:"model"`
	PosX      float64            `json:"posX"` // 开孔中心距台面左边缘 mm
	PosY      float64            `json:"posY"` // 开孔中心距台前边缘 mm
	Width     float64            `json:"width"`
	Height    float64            `json:"height"`
	Images    []MeasurementImage `gorm:"foreignKey:CutoutID" json:"images"`
}

type MeasurementImage struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CutoutID   uint      `gorm:"index" json:"cutoutId"`
	FilePath   string    `json:"filePath"`
	UploadedBy string    `json:"uploadedBy"`
	CreatedAt  time.Time `json:"createdAt"`
}

// 重新测量记录：同一开孔的前后数值与原因
type Remeasure struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrderID   uint      `gorm:"index" json:"orderId"`
	CutoutID  uint      `gorm:"index" json:"cutoutId"`
	Label     string    `json:"label"`
	OldPosX   float64   `json:"oldPosX"`
	OldPosY   float64   `json:"oldPosY"`
	OldWidth  float64   `json:"oldWidth"`
	OldHeight float64   `json:"oldHeight"`
	NewPosX   float64   `json:"newPosX"`
	NewPosY   float64   `json:"newPosY"`
	NewWidth  float64   `json:"newWidth"`
	NewHeight float64   `json:"newHeight"`
	Reason    string    `json:"reason"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// 客户签字确认记录
type Confirmation struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrderID   uint      `gorm:"index" json:"orderId"`
	DrawingID uint      `json:"drawingId"`
	Version   int       `json:"version"`
	Customer  string    `json:"customer"`
	Signature string    `json:"signature"`
	CreatedAt time.Time `json:"createdAt"`
}

// 加工记录：锁定加工所用的图纸版本
type Fabrication struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	OrderID        uint           `gorm:"index" json:"orderId"`
	DrawingID      uint           `json:"drawingId"`
	DrawingVersion int            `json:"drawingVersion"`
	ProcessedBy    string         `json:"processedBy"`
	Note           string         `json:"note"`
	Actuals        []ActualCutout `gorm:"foreignKey:FabricationID" json:"actuals"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// 工厂实际开孔尺寸
type ActualCutout struct {
	ID            uint    `gorm:"primaryKey" json:"id"`
	FabricationID uint    `gorm:"index" json:"fabricationId"`
	CutoutID      uint    `json:"cutoutId"`
	Label         string  `json:"label"`
	PosX          float64 `json:"posX"`
	PosY          float64 `json:"posY"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
}

// 安装现场复测尺寸（可多次，保留历史）
type FieldMeasurement struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	OrderID    uint      `gorm:"index" json:"orderId"`
	CutoutID   uint      `json:"cutoutId"`
	Label      string    `json:"label"`
	PosX       float64   `json:"posX"`
	PosY       float64   `json:"posY"`
	Width      float64   `json:"width"`
	Height     float64   `json:"height"`
	Note       string    `json:"note"`
	MeasuredBy string    `json:"measuredBy"`
	CreatedAt  time.Time `json:"createdAt"`
}

// 返工/变更方案：费用与责任方单独记录，客户确认后才能执行
type ReworkPlan struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	OrderID          uint       `gorm:"index" json:"orderId"`
	CutoutID         uint       `json:"cutoutId"`
	CutoutLabel      string     `json:"cutoutLabel"`
	Type             string     `json:"type"` // rework 返工 / change 变更
	Description      string     `json:"description"`
	Cost             float64    `json:"cost"`
	ResponsibleParty string     `json:"responsibleParty"` // factory/surveyor/customer/installer
	Status           string     `json:"status"`
	ProposedBy       string     `json:"proposedBy"`
	DecidedBy        string     `json:"decidedBy"`
	DecidedAt        *time.Time `json:"decidedAt"`
	CompletedBy      string     `json:"completedBy"`
	CompletedAt      *time.Time `json:"completedAt"`
	CreatedAt        time.Time  `json:"createdAt"`
}

// 操作日志：每次确认、换版、加工、返工都可追溯
type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrderID   uint      `gorm:"index" json:"orderId"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
}
