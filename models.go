package main

import (
	"time"
)

// 角色
const (
	RoleSurveyor  = "surveyor"  // 测量员：测量、出图、换版、复测
	RoleFactory   = "factory"   // 工厂：按客户确认版本加工、执行返工
	RoleInstaller = "installer" // 安装人员：现场测量、提偏差/返工方案、安装
	RoleCustomer  = "customer"  // 客户：确认图纸、确认返工方案、验收
)

// 订单状态
const (
	StatusMeasuring      = "measuring"       // 测量中
	StatusPendingConfirm = "pending_confirm" // 待客户确认
	StatusConfirmed      = "confirmed"       // 图纸已确认，待加工
	StatusProducing      = "producing"       // 加工中
	StatusProduced       = "produced"        // 已加工，待现场安装
	StatusBlocked        = "blocked"         // 安装前发现偏差，处理中
	StatusReadyInstall   = "ready_install"   // 偏差闭环，可安装
	StatusInstalled      = "installed"       // 安装完成，待客户验收
	StatusAccepted       = "accepted"        // 客户验收完成
)

// 图纸版本状态
const (
	VerDraft      = "draft"      // 草稿
	VerSubmitted  = "submitted"  // 已提交客户
	VerConfirmed  = "confirmed"  // 客户已确认（加工基线）
	VerSuperseded = "superseded" // 被新版本替代
	VerRejected   = "rejected"   // 客户驳回
)

// 返工方案状态
const (
	ReworkProposed  = "proposed"
	ReworkConfirmed = "customer_confirmed"
	ReworkRejected  = "rejected"
	ReworkDone      = "done"
)

// 测量阶段
const (
	MeasureInitial       = "initial"        // 初测
	MeasureRemeasure     = "remeasure"      // 复测（保留前后值与原因）
	MeasureFactoryActual = "factory_actual" // 工厂实际开孔
	MeasureSite          = "site"           // 安装现场测量
	MeasurePostRework    = "post_rework"    // 返工后复测
)

// 偏差状态
const (
	DevOpen              = "open"               // 已发现，待提方案
	DevProposalPending   = "proposal_pending"   // 返工/变更方案待客户确认
	DevCustomerConfirmed = "customer_confirmed" // 客户已确认方案，待执行
	DevReworkDone        = "rework_done"        // 返工已执行，待安装方复核
	DevResolved          = "resolved"           // 偏差闭环
	DevRejected          = "rejected"           // 客户驳回
)

type User struct {
	ID           uint   `gorm:"primarykey"`
	Username     string `gorm:"uniqueIndex;not null"`
	PasswordHash string `gorm:"not null"`
	Name         string `gorm:"not null"`
	Role         string `gorm:"not null"`
	CreatedAt    time.Time
}

// Order 台面加工安装工单
type Order struct {
	ID                uint   `gorm:"primarykey"`
	Code              string `gorm:"uniqueIndex;not null"`
	CustomerID        uint
	CustomerName      string
	Address           string
	Status            string `gorm:"not null;index"`
	Note              string
	CurrentVersionID  uint // 当前图纸版本（加工基线，确认时写入）
	ProducedVersionID uint // 实际加工所依据的版本（加工完成后锁定，换版不能自动套用）
	CreatedByID       uint
	CreatedByName     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// DrawingVersion 图纸版本：客户确认后成为加工基线
type DrawingVersion struct {
	ID              uint `gorm:"primarykey"`
	OrderID         uint `gorm:"not null;index"`
	VersionNo       int  `gorm:"not null"`
	Note            string
	Status          string `gorm:"not null"`
	CreatedByID     uint
	CreatedByName   string
	SubmittedAt     *time.Time
	ConfirmedByID   uint
	ConfirmedByName string
	ConfirmedAt     *time.Time
	Openings        []Opening `gorm:"foreignKey:VersionID"`
	CreatedAt       time.Time
}

// Opening 开孔（水槽/灶具），尺寸单位 mm
type Opening struct {
	ID        uint    `gorm:"primarykey"`
	OrderID   uint    `gorm:"not null;index"`
	VersionID uint    `gorm:"not null;index"`
	Slot      string  `gorm:"not null"` // sink / cooktop，跨版本的同一开孔标识
	Kind      string  `gorm:"not null"` // 水槽 / 灶具
	Model     string  `gorm:"not null"` // 型号
	PosX      float64 `json:"pos_x"`    // 开孔左边距（定位）
	PosY      float64 `json:"pos_y"`    // 开孔后边距
	Width     float64
	Depth     float64
	Tolerance float64 // 允许偏差 mm
}

// MeasureRecord 测量记录：复测时保存前后数值与原因
type MeasureRecord struct {
	ID             uint `gorm:"primarykey"`
	OrderID        uint `gorm:"not null;index"`
	VersionID      uint
	Slot           string `gorm:"not null"`
	Stage          string `gorm:"not null"`
	Width          float64
	Depth          float64
	PosX           float64 `json:"pos_x"`
	PosY           float64 `json:"pos_y"`
	PrevWidth      float64 // 复测前数值
	PrevDepth      float64
	PrevPosX       float64 `json:"prev_pos_x"`
	PrevPosY       float64 `json:"prev_pos_y"`
	Reason         string  // 复测/测量原因
	Images         string  `gorm:"type:text"` // JSON 数组：图片 URL
	Note           string
	MeasuredByID   uint
	MeasuredByName string
	CreatedAt      time.Time
}

// Confirmation 客户确认记录（每次确认都留痕）
type Confirmation struct {
	ID         uint `gorm:"primarykey"`
	OrderID    uint `gorm:"not null;index"`
	VersionID  uint
	VersionNo  int
	Action     string `gorm:"not null"` // confirm / reject
	SignedName string
	Note       string
	CustomerID uint
	CreatedAt  time.Time
}

// ProductionRecord 加工记录，锁定加工所依据的图纸版本
type ProductionRecord struct {
	ID           uint `gorm:"primarykey"`
	OrderID      uint `gorm:"not null;index"`
	VersionID    uint `gorm:"not null"`
	VersionNo    int
	Status       string `gorm:"not null"` // in_progress / done
	OperatorID   uint
	OperatorName string
	Note         string
	StartedAt    *time.Time
	FinishedAt   *time.Time
	CreatedAt    time.Time
}

// Deviation 安装前偏差复核记录
type Deviation struct {
	ID           uint   `gorm:"primarykey"`
	OrderID      uint   `gorm:"not null;index"`
	Slot         string `gorm:"not null"`
	Kind         string
	Tolerance    float64
	ExpectWidth  float64 `json:"expect_width"`
	ExpectDepth  float64 `json:"expect_depth"`
	ExpectPosX   float64 `json:"expect_pos_x"`
	ExpectPosY   float64 `json:"expect_pos_y"`
	ActualWidth  float64 `json:"actual_width"`
	ActualDepth  float64 `json:"actual_depth"`
	ActualPosX   float64 `json:"actual_pos_x"`
	ActualPosY   float64 `json:"actual_pos_y"`
	DeltaWidth   float64 `json:"delta_width"`
	DeltaDepth   float64 `json:"delta_depth"`
	DeltaPosX    float64 `json:"delta_pos_x"`
	DeltaPosY    float64 `json:"delta_pos_y"`
	Exceeded     bool    `gorm:"not null"`
	Note         string
	Status       string `gorm:"not null;index"`
	FoundByID    uint
	FoundByName  string
	MeasureID    uint          // 触发偏差的现场测量记录
	ReworkEvents []ReworkEvent `gorm:"foreignKey:DeviationID"`
	CreatedAt    time.Time
	ResolvedAt   *time.Time
}

// ReworkEvent 返工 / 变更方案：客户确认后方可执行
type ReworkEvent struct {
	ID                      uint   `gorm:"primarykey"`
	OrderID                 uint   `gorm:"not null;index"`
	DeviationID             uint   `gorm:"not null"`
	Type                    string `gorm:"not null"` // rework 返工 / change 变更
	Plan                    string `gorm:"type:text"`
	Status                  string `gorm:"not null"` // proposed / customer_confirmed / rejected / done
	EstimatedCost           float64
	ResponsibleParty        string // factory / surveyor / installer / customer
	ProposedByID            uint
	ProposedByName          string
	CustomerConfirmedByID   uint
	CustomerConfirmedByName string
	ConfirmedAt             *time.Time
	RejectedReason          string
	ExecutedByID            uint
	ExecutedByName          string
	ExecutedAt              *time.Time
	ResultNote              string
	CreatedAt               time.Time
}

// CostRecord 费用与责任方单独记录
type CostRecord struct {
	ID               uint `gorm:"primarykey"`
	OrderID          uint `gorm:"not null;index"`
	ReworkEventID    uint
	Amount           float64 `gorm:"not null"`
	Category         string  `gorm:"not null"` // rework / change / material
	ResponsibleParty string  `gorm:"not null"`
	Note             string
	CreatedByID      uint
	CreatedByName    string
	CreatedAt        time.Time
}

// InstallationRecord 安装记录
type InstallationRecord struct {
	ID              uint `gorm:"primarykey"`
	OrderID         uint `gorm:"uniqueIndex"`
	InstalledByID   uint
	InstalledByName string
	Note            string
	InstalledAt     time.Time
	CreatedAt       time.Time
}

// AcceptanceRecord 客户验收记录
type AcceptanceRecord struct {
	ID             uint `gorm:"primarykey"`
	OrderID        uint `gorm:"uniqueIndex"`
	AcceptedByID   uint
	AcceptedByName string
	Note           string
	AcceptedAt     time.Time
	CreatedAt      time.Time
}
