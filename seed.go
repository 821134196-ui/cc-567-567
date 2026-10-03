package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"time"
)

// 生成一张示意测量图片：台面底色 + 网格 + 开孔矩形
func seedImage(path string, w, h float64) {
	const W, H = 480, 360
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	bg := color.RGBA{0xf2, 0xee, 0xe4, 0xff}
	grid := color.RGBA{0xd8, 0xd2, 0xc4, 0xff}
	line := color.RGBA{0x2b, 0x4c, 0x7e, 0xff}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			img.Set(x, y, bg)
		}
	}
	for x := 0; x < W; x += 40 {
		for y := 0; y < H; y++ {
			img.Set(x, y, grid)
		}
	}
	for y := 0; y < H; y += 40 {
		for x := 0; x < W; x++ {
			img.Set(x, y, grid)
		}
	}
	// 开孔按比例画在中央（台面按 2000mm 宽示意）
	scale := float64(W) / 2000.0
	rw := int(w * scale)
	rh := int(h * scale)
	x0 := (W - rw) / 2
	y0 := (H - rh) / 2
	for i := 0; i < 3; i++ {
		for x := x0 - i; x < x0+rw+i; x++ {
			img.Set(x, y0-i, line)
			img.Set(x, y0+rh+i, line)
		}
		for y := y0 - i; y < y0+rh+i; y++ {
			img.Set(x0-i, y, line)
			img.Set(x0+rw+i, y, line)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	_ = png.Encode(f, img)
}

func seed() {
	var count int64
	db.Model(&User{}).Count(&count)
	if count > 0 {
		return
	}
	log.Println("初始化演示数据...")

	users := []User{
		{Username: "surveyor", Password: "123456", Name: "王测", Role: RoleSurveyor},
		{Username: "factory", Password: "123456", Name: "李厂", Role: RoleFactory},
		{Username: "installer", Password: "123456", Name: "赵装", Role: RoleInstaller},
		{Username: "customer1", Password: "123456", Name: "张女士", Role: RoleCustomer},
		{Username: "customer2", Password: "123456", Name: "刘先生", Role: RoleCustomer},
		{Username: "customer3", Password: "123456", Name: "王女士", Role: RoleCustomer},
		{Username: "customer4", Password: "123456", Name: "陈先生", Role: RoleCustomer},
	}
	for i := range users {
		db.Create(&users[i])
	}
	surveyor, factory, installer := users[0], users[1], users[2]
	cust1, cust2, cust3, cust4 := users[3], users[4], users[5], users[6]

	now := time.Now()
	at := func(hoursAgo float64) time.Time { return now.Add(-time.Duration(hoursAgo * float64(time.Hour))) }

	mkCutout := func(drawingID uint, kind, model string, x, y, w, h float64) Cutout {
		ct := Cutout{DrawingID: drawingID, Kind: kind, Model: model, PosX: x, PosY: y, Width: w, Height: h}
		db.Create(&ct)
		return ct
	}
	mkImage := func(ct Cutout, w, h float64, by string, t time.Time) {
		p := fmt.Sprintf("uploads/seed_cutout_%d.png", ct.ID)
		seedImage(p, w, h)
		db.Create(&MeasurementImage{CutoutID: ct.ID, FilePath: "/" + p, UploadedBy: by, CreatedAt: t})
	}
	logAt := func(orderID uint, actor, action, detail string, t time.Time) {
		db.Create(&AuditLog{OrderID: orderID, Actor: actor, Action: action, Detail: detail, CreatedAt: t})
	}

	// ========== 例1 ORD-2026-001：尺寸超差，返工方案待客户确认 ==========
	o1 := Order{OrderNo: "ORD-2026-001", CustomerName: "张女士", CustomerUserID: cust1.ID,
		Address: "滨江区江南大道 88 号 3-1201", Status: OrderReworkPending, CreatedAt: at(96)}
	db.Create(&o1)
	d1 := Drawing{OrderID: o1.ID, Version: 1, Status: DrawingConfirmed, Note: "初测",
		CreatedBy: surveyor.Name, ConfirmedBy: cust1.Name, CreatedAt: at(96)}
	confirmedAt1 := at(90)
	d1.ConfirmedAt = &confirmedAt1
	db.Create(&d1)
	c1a := mkCutout(d1.ID, "sink", "SK-760 单槽", 400, 250, 760, 460)
	c1b := mkCutout(d1.ID, "cooktop", "CK-700 双眼灶", 1300, 260, 700, 400)
	mkImage(c1a, 760, 460, surveyor.Name, at(95))
	mkImage(c1b, 700, 400, surveyor.Name, at(95))
	db.Create(&Confirmation{OrderID: o1.ID, DrawingID: d1.ID, Version: 1, Customer: cust1.Name, Signature: "张丽", CreatedAt: at(90)})
	o1.CurrentDrawingID = &d1.ID
	db.Save(&o1)
	fab1 := Fabrication{OrderID: o1.ID, DrawingID: d1.ID, DrawingVersion: 1,
		ProcessedBy: factory.Name, Note: "常规加工", CreatedAt: at(72)}
	db.Create(&fab1)
	// 水槽实际开孔宽了 6mm（超差），灶具正常
	db.Create(&ActualCutout{FabricationID: fab1.ID, CutoutID: c1a.ID, Label: cutoutLabel(c1a.Kind, c1a.Model), PosX: 400, PosY: 250, Width: 766, Height: 460})
	db.Create(&ActualCutout{FabricationID: fab1.ID, CutoutID: c1b.ID, Label: cutoutLabel(c1b.Kind, c1b.Model), PosX: 1300, PosY: 260, Width: 700, Height: 400})
	db.Create(&FieldMeasurement{OrderID: o1.ID, CutoutID: c1a.ID, Label: cutoutLabel(c1a.Kind, c1a.Model),
		PosX: 400, PosY: 250, Width: 766, Height: 459, Note: "现场复测与出厂一致，水槽放不进去", MeasuredBy: installer.Name, CreatedAt: at(24)})
	db.Create(&FieldMeasurement{OrderID: o1.ID, CutoutID: c1b.ID, Label: cutoutLabel(c1b.Kind, c1b.Model),
		PosX: 1300, PosY: 260, Width: 700, Height: 400, Note: "正常", MeasuredBy: installer.Name, CreatedAt: at(24)})
	db.Create(&ReworkPlan{OrderID: o1.ID, CutoutID: c1a.ID, CutoutLabel: cutoutLabel(c1a.Kind, c1a.Model),
		Type: "rework", Description: "水槽开孔宽度超差 6mm：台面返厂，沿长度方向扩孔至 760mm 并重新封边打磨",
		Cost: 800, ResponsibleParty: RoleFactory, Status: PlanPending, ProposedBy: installer.Name, CreatedAt: at(23)})
	logAt(o1.ID, surveyor.Name, "创建订单", "创建订单 ORD-2026-001 并录入初测数据（图纸 v1 草稿）", at(96))
	logAt(o1.ID, surveyor.Name, "提交确认", "图纸 v1 提交客户确认", at(92))
	logAt(o1.ID, cust1.Name, "客户确认图纸", "客户签字确认图纸 v1，订单进入待加工", at(90))
	logAt(o1.ID, factory.Name, "加工完成", "按图纸 v1 完成加工，该版本尺寸已锁定", at(72))
	logAt(o1.ID, installer.Name, "现场测量", "开孔 水槽 SK-760 单槽 现场复测 766×459（超差）", at(24))
	logAt(o1.ID, installer.Name, "提出方案", "针对开孔 水槽 SK-760 单槽 提出返工方案，费用 800 元，待客户确认", at(23))

	// ========== 例2 ORD-2026-002：图纸已提交，客户尚未确认（含重新测量记录） ==========
	o2 := Order{OrderNo: "ORD-2026-002", CustomerName: "刘先生", CustomerUserID: cust2.ID,
		Address: "西湖区文三路 199 号 7-402", Status: OrderPendingConfirm, CreatedAt: at(30)}
	db.Create(&o2)
	d2 := Drawing{OrderID: o2.ID, Version: 1, Status: DrawingPending, Note: "初测",
		CreatedBy: surveyor.Name, CreatedAt: at(30)}
	db.Create(&d2)
	c2a := mkCutout(d2.ID, "sink", "SK-680 双槽", 380, 240, 760, 460)
	c2b := mkCutout(d2.ID, "cooktop", "CK-650 双眼灶", 1250, 250, 650, 380)
	mkImage(c2a, 760, 460, surveyor.Name, at(29))
	mkImage(c2b, 650, 380, surveyor.Name, at(29))
	// 同一开孔的重新测量：首次 750，复测修正为 760
	db.Create(&Remeasure{OrderID: o2.ID, CutoutID: c2a.ID, Label: cutoutLabel(c2a.Kind, c2a.Model),
		OldPosX: 380, OldPosY: 240, OldWidth: 750, OldHeight: 460,
		NewPosX: 380, NewPosY: 240, NewWidth: 760, NewHeight: 460,
		Reason: "首次卷尺读数误差，复测修正为 760mm", CreatedBy: surveyor.Name, CreatedAt: at(28)})
	logAt(o2.ID, surveyor.Name, "创建订单", "创建订单 ORD-2026-002 并录入初测数据（图纸 v1 草稿）", at(30))
	logAt(o2.ID, surveyor.Name, "重新测量", "开孔 水槽 SK-680 双槽 复测（原因：首次卷尺读数误差，复测修正为 760mm）", at(28))
	logAt(o2.ID, surveyor.Name, "提交确认", "图纸 v1 提交客户确认", at(26))

	// ========== 例3 ORD-2026-003：加工后换版，新尺寸不自动套用 ==========
	o3 := Order{OrderNo: "ORD-2026-003", CustomerName: "王女士", CustomerUserID: cust3.ID,
		Address: "拱墅区莫干山路 66 号 1-801", Status: OrderProcessed, CreatedAt: at(120)}
	db.Create(&o3)
	d3v1 := Drawing{OrderID: o3.ID, Version: 1, Status: DrawingSuperseded, Note: "初测",
		CreatedBy: surveyor.Name, ConfirmedBy: cust3.Name, CreatedAt: at(120)}
	confirmedAt3 := at(118)
	d3v1.ConfirmedAt = &confirmedAt3
	db.Create(&d3v1)
	c3a := mkCutout(d3v1.ID, "sink", "SK-760 单槽", 400, 250, 760, 460)
	c3b := mkCutout(d3v1.ID, "cooktop", "CK-700 双眼灶", 1300, 260, 700, 400)
	mkImage(c3a, 760, 460, surveyor.Name, at(119))
	db.Create(&Confirmation{OrderID: o3.ID, DrawingID: d3v1.ID, Version: 1, Customer: cust3.Name, Signature: "王芳", CreatedAt: at(118)})
	o3.CurrentDrawingID = &d3v1.ID
	db.Save(&o3)
	fab3 := Fabrication{OrderID: o3.ID, DrawingID: d3v1.ID, DrawingVersion: 1,
		ProcessedBy: factory.Name, Note: "常规加工", CreatedAt: at(100)}
	db.Create(&fab3)
	db.Create(&ActualCutout{FabricationID: fab3.ID, CutoutID: c3a.ID, Label: cutoutLabel(c3a.Kind, c3a.Model), PosX: 400, PosY: 250, Width: 760, Height: 460})
	db.Create(&ActualCutout{FabricationID: fab3.ID, CutoutID: c3b.ID, Label: cutoutLabel(c3b.Kind, c3b.Model), PosX: 1300, PosY: 260, Width: 700, Height: 400})
	// 加工完成后客户要求水槽右移 20mm → v2 确认，但台面已按 v1 加工，不自动套用
	d3v2 := Drawing{OrderID: o3.ID, Version: 2, Status: DrawingConfirmed, Note: "客户要求水槽右移 20mm",
		CreatedBy: surveyor.Name, ConfirmedBy: cust3.Name, CreatedAt: at(50)}
	confirmedAt3v2 := at(48)
	d3v2.ConfirmedAt = &confirmedAt3v2
	db.Create(&d3v2)
	mkCutout(d3v2.ID, "sink", "SK-760 单槽", 420, 250, 760, 460)
	mkCutout(d3v2.ID, "cooktop", "CK-700 双眼灶", 1300, 260, 700, 400)
	db.Create(&Confirmation{OrderID: o3.ID, DrawingID: d3v2.ID, Version: 2, Customer: cust3.Name, Signature: "王芳", CreatedAt: at(48)})
	logAt(o3.ID, surveyor.Name, "创建订单", "创建订单 ORD-2026-003 并录入初测数据（图纸 v1 草稿）", at(120))
	logAt(o3.ID, cust3.Name, "客户确认图纸", "客户签字确认图纸 v1，订单进入待加工", at(118))
	logAt(o3.ID, factory.Name, "加工完成", "按图纸 v1 完成加工，该版本尺寸已锁定", at(100))
	logAt(o3.ID, surveyor.Name, "图纸换版", "创建图纸 v2 草稿（基于 v1，原因：客户要求水槽右移 20mm）", at(50))
	logAt(o3.ID, cust3.Name, "客户确认图纸", "客户签字确认图纸 v2；台面已按旧版加工，新尺寸不自动套用", at(48))

	// ========== 例4 ORD-2026-004：返工方案经客户批准并执行完成，订单已验收 ==========
	o4 := Order{OrderNo: "ORD-2026-004", CustomerName: "陈先生", CustomerUserID: cust4.ID,
		Address: "上城区庆春路 12 号 5-602", Status: OrderCompleted, CreatedAt: at(200)}
	db.Create(&o4)
	d4 := Drawing{OrderID: o4.ID, Version: 1, Status: DrawingConfirmed, Note: "初测",
		CreatedBy: surveyor.Name, ConfirmedBy: cust4.Name, CreatedAt: at(200)}
	confirmedAt4 := at(196)
	d4.ConfirmedAt = &confirmedAt4
	db.Create(&d4)
	c4a := mkCutout(d4.ID, "sink", "SK-760 单槽", 400, 250, 760, 460)
	c4b := mkCutout(d4.ID, "cooktop", "CK-700 双眼灶", 1300, 260, 700, 400)
	mkImage(c4a, 760, 460, surveyor.Name, at(199))
	mkImage(c4b, 700, 400, surveyor.Name, at(199))
	db.Create(&Confirmation{OrderID: o4.ID, DrawingID: d4.ID, Version: 1, Customer: cust4.Name, Signature: "陈强", CreatedAt: at(196)})
	o4.CurrentDrawingID = &d4.ID
	db.Save(&o4)
	fab4 := Fabrication{OrderID: o4.ID, DrawingID: d4.ID, DrawingVersion: 1,
		ProcessedBy: factory.Name, Note: "常规加工", CreatedAt: at(180)}
	db.Create(&fab4)
	db.Create(&ActualCutout{FabricationID: fab4.ID, CutoutID: c4a.ID, Label: cutoutLabel(c4a.Kind, c4a.Model), PosX: 400, PosY: 250, Width: 760, Height: 460})
	// 灶具实际开孔宽了 6mm（超差）
	db.Create(&ActualCutout{FabricationID: fab4.ID, CutoutID: c4b.ID, Label: cutoutLabel(c4b.Kind, c4b.Model), PosX: 1300, PosY: 260, Width: 706, Height: 400})
	db.Create(&FieldMeasurement{OrderID: o4.ID, CutoutID: c4b.ID, Label: cutoutLabel(c4b.Kind, c4b.Model),
		PosX: 1300, PosY: 260, Width: 706, Height: 400, Note: "灶具开孔超差，需现场扩孔", MeasuredBy: installer.Name, CreatedAt: at(150)})
	decidedAt4 := at(140)
	completedAt4 := at(130)
	db.Create(&ReworkPlan{OrderID: o4.ID, CutoutID: c4b.ID, CutoutLabel: cutoutLabel(c4b.Kind, c4b.Model),
		Type: "rework", Description: "灶具开孔宽度超差 6mm：现场扩孔至 700mm 标准尺寸并加固封边",
		Cost: 500, ResponsibleParty: RoleFactory, Status: PlanCompleted, ProposedBy: installer.Name,
		DecidedBy: cust4.Name, DecidedAt: &decidedAt4, CompletedBy: installer.Name, CompletedAt: &completedAt4, CreatedAt: at(149)})
	logAt(o4.ID, surveyor.Name, "创建订单", "创建订单 ORD-2026-004 并录入初测数据（图纸 v1 草稿）", at(200))
	logAt(o4.ID, cust4.Name, "客户确认图纸", "客户签字确认图纸 v1，订单进入待加工", at(196))
	logAt(o4.ID, factory.Name, "加工完成", "按图纸 v1 完成加工，该版本尺寸已锁定", at(180))
	logAt(o4.ID, installer.Name, "现场测量", "开孔 灶具 CK-700 双眼灶 现场复测 706×400（超差）", at(150))
	logAt(o4.ID, installer.Name, "提出方案", "针对开孔 灶具 CK-700 双眼灶 提出返工方案，费用 500 元，待客户确认", at(149))
	logAt(o4.ID, cust4.Name, "客户审批方案", "客户批准了针对开孔 灶具 CK-700 双眼灶 的方案", at(140))
	logAt(o4.ID, installer.Name, "返工完成", "开孔 灶具 CK-700 双眼灶 的方案已执行完成", at(130))
	logAt(o4.ID, installer.Name, "安装验收", "安装验收完成，订单关闭", at(128))

	log.Println("演示数据初始化完成：4 个订单（超差 / 未确认 / 加工后换版 / 返工完成）")
}
