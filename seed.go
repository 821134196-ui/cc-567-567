package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// 1x1 占位 PNG，作为本地测量图片示例
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC"

func seedUsers() {
	users := []struct{ user, pass, name, role string }{
		{"surveyor", "sur12345", "张测（测量员）", RoleSurveyor},
		{"factory", "fac12345", "李厂（工厂）", RoleFactory},
		{"installer", "ins12345", "王装（安装人员）", RoleInstaller},
		{"customer", "cus12345", "陈先生（客户）", RoleCustomer},
	}
	for _, s := range users {
		var cnt int64
		db.Model(&User{}).Where("username = ?", s.user).Count(&cnt)
		if cnt == 0 {
			db.Create(&User{
				Username: s.user, PasswordHash: hashPassword(s.pass),
				Name: s.name, Role: s.role,
			})
		}
	}
}

func mustUser(name string) User {
	var u User
	db.Where("username = ?", name).First(&u)
	return u
}

func seedImage(name string) string {
	full := filepath.Join(UploadDir, name)
	if _, err := os.Stat(full); os.IsNotExist(err) {
		b, _ := base64.StdEncoding.DecodeString(tinyPNG)
		_ = os.WriteFile(full, b, 0o644)
	}
	return "/uploads/" + name
}

func seedData() error {
	var orderCnt int64
	db.Model(&Order{}).Count(&orderCnt)
	if orderCnt > 0 {
		return nil
	}

	sur := mustUser("surveyor")
	fac := mustUser("factory")
	ins := mustUser("installer")
	cus := mustUser("customer")
	t0 := time.Now().Add(-72 * time.Hour)

	img1 := seedImage("seed_sink_initial.png")
	img2 := seedImage("seed_sink_remeasure.png")
	img3 := seedImage("seed_site_oversize.png")
	img4 := seedImage("seed_cooktop_site.png")
	img5 := seedImage("seed_post_rework.png")

	create := func(code, addr, note string, status string, createdAt time.Time) Order {
		o := Order{
			Code: code, CustomerID: cus.ID, CustomerName: cus.Name, Address: addr,
			Note: note, Status: status, CreatedByID: sur.ID, CreatedByName: sur.Name,
			CreatedAt: createdAt, UpdatedAt: createdAt,
		}
		db.Create(&o)
		return o
	}
	version := func(o Order, no int, status, note string, createdAt time.Time, confirmed bool) DrawingVersion {
		v := DrawingVersion{
			OrderID: o.ID, VersionNo: no, Note: note, Status: status,
			CreatedByID: sur.ID, CreatedByName: sur.Name, CreatedAt: createdAt,
		}
		if status == VerSubmitted || confirmed {
			v.SubmittedAt = nullTime(createdAt.Add(2 * time.Hour))
		}
		if confirmed {
			v.Status = VerConfirmed
			v.ConfirmedByID = cus.ID
			v.ConfirmedByName = cus.Name
			v.ConfirmedAt = nullTime(createdAt.Add(6 * time.Hour))
		}
		db.Create(&v)
		return v
	}
	opening := func(v DrawingVersion, slot, kind, model string, x, y, w, d, tol float64) Opening {
		op := Opening{
			OrderID: v.OrderID, VersionID: v.ID, Slot: slot, Kind: kind, Model: model,
			PosX: x, PosY: y, Width: w, Depth: d, Tolerance: tol,
		}
		db.Create(&op)
		return op
	}
	measure := func(o Order, vid uint, slot, stage string, x, y, w, d float64,
		reason, images, note string, by User, at time.Time, prev *[4]float64) MeasureRecord {
		m := MeasureRecord{
			OrderID: o.ID, VersionID: vid, Slot: slot, Stage: stage,
			Width: w, Depth: d, PosX: x, PosY: y,
			Reason: reason, Images: images, Note: note,
			MeasuredByID: by.ID, MeasuredByName: by.Name, CreatedAt: at,
		}
		if prev != nil {
			m.PrevPosX, m.PrevPosY, m.PrevWidth, m.PrevDepth = prev[0], prev[1], prev[2], prev[3]
		}
		db.Create(&m)
		return m
	}

	// ---------- 例1：尺寸超差（现场发现，偏差待处理，禁止安装） ----------
	o1 := create("O-DEV-01", "阳光花园 3-201", "例1：现场复核开孔宽度超差，待提返工方案", StatusProduced, t0)
	v1 := version(o1, 1, VerConfirmed, "首版尺寸图（客户签字）", t0.Add(1*time.Hour), true)
	opening(v1, "sink", "水槽", "欧琳 OL-7843", 120, 80, 760, 450, 2)
	opening(v1, "cooktop", "灶具", "方太 TH26B", 100, 560, 680, 380, 2)
	db.Model(&Order{}).Where("id = ?", o1.ID).Updates(map[string]any{
		"current_version_id": v1.ID, "produced_version_id": v1.ID,
	})
	db.Create(&Confirmation{
		OrderID: o1.ID, VersionID: v1.ID, VersionNo: 1, Action: "confirm",
		SignedName: cus.Name, Note: "客户现场签字确认", CustomerID: cus.ID, CreatedAt: t0.Add(7 * time.Hour),
	})
	measure(o1, v1.ID, "sink", MeasureInitial, 120, 80, 760, 450, "", "["+quote(img1)+"]",
		"初测照片", sur, t0.Add(2*time.Hour), nil)
	db.Create(&ProductionRecord{
		OrderID: o1.ID, VersionID: v1.ID, VersionNo: 1, Status: "done",
		OperatorID: fac.ID, OperatorName: fac.Name, Note: "按 V1 加工",
		StartedAt: nullTime(t0.Add(24 * time.Hour)), FinishedAt: nullTime(t0.Add(48 * time.Hour)),
		CreatedAt: t0.Add(24 * time.Hour),
	})
	measure(o1, v1.ID, "sink", MeasureFactoryActual, 120, 80, 758, 450, "", "",
		"工厂自检：宽度 758（公差内）", fac, t0.Add(48*time.Hour), nil)
	// 现场测量 765，对比确认值 760，偏差 +5mm > 公差 2mm
	site1 := measure(o1, v1.ID, "sink", MeasureSite, 120, 80, 765, 450, "", "["+quote(img3)+"]",
		"现场复尺发现水槽孔偏宽", ins, t0.Add(70*time.Hour), nil)
	db.Create(&Deviation{
		OrderID: o1.ID, Slot: "sink", Kind: "水槽", Tolerance: 2,
		ExpectWidth: 760, ExpectDepth: 450, ExpectPosX: 120, ExpectPosY: 80,
		ActualWidth: 765, ActualDepth: 450, ActualPosX: 120, ActualPosY: 80,
		DeltaWidth: 5, Exceeded: true, Note: "宽度 +5mm，超出 ±2mm 公差",
		Status: DevOpen, FoundByID: ins.ID, FoundByName: ins.Name, MeasureID: site1.ID,
		CreatedAt: t0.Add(70 * time.Hour),
	})
	db.Model(&Order{}).Where("id = ?", o1.ID).Update("status", StatusBlocked)

	// ---------- 例2：客户尚未确认（含同一开孔复测前后数值与原因） ----------
	o2 := create("O-NEW-02", "湖畔林语 7-1502", "例2：V1 已提交，客户尚未签字确认；含水槽复测记录", StatusPendingConfirm, t0.Add(10*time.Hour))
	v2 := version(o2, 1, VerSubmitted, "首版（复测后更新水槽尺寸）", t0.Add(12*time.Hour), false)
	opening(v2, "sink", "水槽", "摩恩 SW-760", 100, 90, 760, 450, 2)
	opening(v2, "cooktop", "灶具", "老板 30B6X", 110, 580, 700, 400, 2)
	// 初测 -> 复测，前后数值与原因都保留
	measure(o2, 0, "sink", MeasureInitial, 100, 90, 750, 440, "", "["+quote(img1)+"]",
		"初测：按旧型号", sur, t0.Add(12*time.Hour), nil)
	pv := [4]float64{100, 90, 750, 440}
	measure(o2, v2.ID, "sink", MeasureRemeasure, 100, 90, 760, 450,
		"客户更换为更大的单槽水槽，安装前现场复尺", "["+quote(img2)+"]",
		"复测值用于 V1 图纸", sur, t0.Add(20*time.Hour), &pv)
	// 工厂此时尝试开工将被拒绝（客户尚未确认）——核对脚本中验证

	// ---------- 例3：加工后换版（新图纸不能自动套用已加工台面） ----------
	o3 := create("O-VER-03", "江景苑 1-603", "例3：已按 V1 加工完成，测量员出 V2 必须被拦截", StatusProduced, t0.Add(20*time.Hour))
	v3 := version(o3, 1, VerConfirmed, "首版", t0.Add(21*time.Hour), true)
	opening(v3, "cooktop", "灶具", "美的 Q310", 90, 540, 650, 350, 2)
	db.Model(&Order{}).Where("id = ?", o3.ID).Updates(map[string]any{
		"current_version_id": v3.ID, "produced_version_id": v3.ID,
	})
	db.Create(&Confirmation{
		OrderID: o3.ID, VersionID: v3.ID, VersionNo: 1, Action: "confirm",
		SignedName: cus.Name, CustomerID: cus.ID, CreatedAt: t0.Add(28 * time.Hour),
	})
	db.Create(&ProductionRecord{
		OrderID: o3.ID, VersionID: v3.ID, VersionNo: 1, Status: "done",
		OperatorID: fac.ID, OperatorName: fac.Name,
		StartedAt: nullTime(t0.Add(30 * time.Hour)), FinishedAt: nullTime(t0.Add(40 * time.Hour)),
		CreatedAt: t0.Add(30 * time.Hour),
	})
	measure(o3, v3.ID, "cooktop", MeasureFactoryActual, 90, 540, 650, 350, "", "",
		"已按 V1 加工完成", fac, t0.Add(40*time.Hour), nil)
	// 期望的 V2（灶具改 700x400）保存在备注里说明，但不允许落库——由核对脚本验证 409

	// ---------- 例4：返工全流程（客户确认→工厂返工→费用责任→复核→安装验收） ----------
	o4 := create("O-RWK-04", "梧桐里 5-808", "例4：灶具孔超差返工完成并已验收（完整留痕）", StatusAccepted, t0.Add(30*time.Hour))
	v4 := version(o4, 1, VerConfirmed, "首版尺寸图", t0.Add(31*time.Hour), true)
	opening(v4, "cooktop", "灶具", "华帝 i10051B", 100, 560, 680, 380, 2)
	db.Model(&Order{}).Where("id = ?", o4.ID).Updates(map[string]any{
		"current_version_id": v4.ID, "produced_version_id": v4.ID,
	})
	db.Create(&Confirmation{
		OrderID: o4.ID, VersionID: v4.ID, VersionNo: 1, Action: "confirm",
		SignedName: cus.Name, CustomerID: cus.ID, CreatedAt: t0.Add(37 * time.Hour),
	})
	db.Create(&ProductionRecord{
		OrderID: o4.ID, VersionID: v4.ID, VersionNo: 1, Status: "done",
		OperatorID: fac.ID, OperatorName: fac.Name,
		StartedAt: nullTime(t0.Add(40 * time.Hour)), FinishedAt: nullTime(t0.Add(50 * time.Hour)),
		CreatedAt: t0.Add(40 * time.Hour),
	})
	measure(o4, v4.ID, "cooktop", MeasureFactoryActual, 100, 560, 680, 380, "", "",
		"工厂自检合格", fac, t0.Add(50*time.Hour), nil)
	site4 := measure(o4, v4.ID, "cooktop", MeasureSite, 100, 560, 686, 380, "", "["+quote(img4)+"]",
		"灶具孔宽度方向 +6mm", ins, t0.Add(58*time.Hour), nil)
	dev4 := Deviation{
		OrderID: o4.ID, Slot: "cooktop", Kind: "灶具", Tolerance: 2,
		ExpectWidth: 680, ExpectDepth: 380, ExpectPosX: 100, ExpectPosY: 560,
		ActualWidth: 686, ActualDepth: 380, ActualPosX: 100, ActualPosY: 560,
		DeltaWidth: 6, Exceeded: true, Note: "宽度 +6mm，超差",
		Status: DevResolved, FoundByID: ins.ID, FoundByName: ins.Name, MeasureID: site4.ID,
		CreatedAt: t0.Add(58 * time.Hour), ResolvedAt: nullTime(t0.Add(66 * time.Hour)),
	}
	db.Create(&dev4)
	rw4 := ReworkEvent{
		OrderID: dev4.OrderID, DeviationID: dev4.ID, Type: "rework",
		Plan:   "返厂在宽度方向补衬条并重新修边，使开孔恢复 680±2mm，不接受现场直接扩孔",
		Status: ReworkDone, EstimatedCost: 300, ResponsibleParty: "factory",
		ProposedByID: ins.ID, ProposedByName: ins.Name, CreatedAt: t0.Add(59 * time.Hour),
		CustomerConfirmedByID: cus.ID, CustomerConfirmedByName: cus.Name,
		ConfirmedAt:  nullTime(t0.Add(61 * time.Hour)),
		ExecutedByID: fac.ID, ExecutedByName: fac.Name, ExecutedAt: nullTime(t0.Add(64 * time.Hour)),
		ResultNote: "补条修边完成，复测 680mm",
	}
	db.Create(&rw4)
	db.Create(&CostRecord{
		OrderID: o4.ID, ReworkEventID: rw4.ID, Amount: 300, Category: "rework",
		ResponsibleParty: "factory", Note: "返工工时+衬条材料，责任方：工厂",
		CreatedByID: fac.ID, CreatedByName: fac.Name, CreatedAt: t0.Add(62 * time.Hour),
	})
	measure(o4, v4.ID, "cooktop", MeasurePostRework, 100, 560, 680, 380, "", "["+quote(img5)+"]",
		"返工后安装方复测合格", ins, t0.Add(65*time.Hour), nil)
	db.Create(&InstallationRecord{
		OrderID: o4.ID, InstalledByID: ins.ID, InstalledByName: ins.Name,
		Note: "返工台面复装", InstalledAt: t0.Add(68 * time.Hour), CreatedAt: t0.Add(68 * time.Hour),
	})
	db.Create(&AcceptanceRecord{
		OrderID: o4.ID, AcceptedByID: cus.ID, AcceptedByName: cus.Name,
		Note: "现场试装合格，签字验收", AcceptedAt: t0.Add(70 * time.Hour), CreatedAt: t0.Add(70 * time.Hour),
	})

	return nil
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
