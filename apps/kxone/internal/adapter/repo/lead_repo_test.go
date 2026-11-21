package repo

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestGormLeadRepositoryUpdatePersistsZeroValues(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "leads.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewGormLeadRepository(db)
	ctx := context.Background()
	now := time.Now()
	claimedAt := now.Add(time.Minute)
	lead := &types.Lead{
		LeadNo:                "L-ZERO-1",
		CustomerName:          "测试客户",
		Status:                types.LeadStatusClaimed,
		Version:               1,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		OwnerUserID:           "sales1",
		ClaimedAt:             &claimedAt,
		DealAmount:            1200,
		CloseReason:           "已成交",
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := repo.Create(ctx, lead); err != nil {
		t.Fatalf("create lead: %v", err)
	}

	lead.Status = types.LeadStatusAssignedPendingClaim
	lead.OwnerUserID = ""
	lead.ClaimedAt = nil
	lead.DealAmount = 0
	lead.CloseReason = ""
	lead.Version++
	lead.UpdatedAt = now.Add(2 * time.Minute)
	if err := repo.Update(ctx, lead); err != nil {
		t.Fatalf("update lead: %v", err)
	}

	got, err := repo.GetByID(ctx, lead.ID)
	if err != nil {
		t.Fatalf("get lead: %v", err)
	}
	if got.OwnerUserID != "" {
		t.Fatalf("owner_user_id = %q, want empty", got.OwnerUserID)
	}
	if got.ClaimedAt != nil {
		t.Fatalf("claimed_at = %v, want nil", got.ClaimedAt)
	}
	if got.DealAmount != 0 {
		t.Fatalf("deal_amount = %v, want 0", got.DealAmount)
	}
	if got.CloseReason != "" {
		t.Fatalf("close_reason = %q, want empty", got.CloseReason)
	}
	if got.Version != 2 {
		t.Fatalf("version = %d, want 2", got.Version)
	}
}

func TestGormLeadRepositoryUpdateKeepsUnchangedFields(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "leads-unchanged.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewGormLeadRepository(db)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	assignedAt := now.Add(time.Minute)
	claimedAt := now.Add(2 * time.Minute)
	firstFollowupAt := now.Add(3 * time.Minute)
	lead := &types.Lead{
		LeadNo:                "L-UNCHANGED-1",
		CustomerName:          "保持字段客户",
		CustomerMobile:        "13800000000",
		CustomerMobileMasked:  "138****0000",
		City:                  "杭州",
		ProductName:           "课程A",
		SourceChannel:         "ad:baidu",
		SourceCampaign:        "计划 / 单元 / 创意",
		LeadLevel:             "high",
		Status:                types.LeadStatusClaimed,
		Version:               1,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		OwnerUserID:           "sales1",
		CreatedAt:             now,
		AssignedAt:            &assignedAt,
		ClaimedAt:             &claimedAt,
		FirstFollowupAt:       &firstFollowupAt,
		DealAmount:            999,
		CloseReason:           "保留原因",
		UpdatedAt:             now,
	}
	if err := repo.Create(ctx, lead); err != nil {
		t.Fatalf("create lead: %v", err)
	}

	loaded, err := repo.GetByID(ctx, lead.ID)
	if err != nil {
		t.Fatalf("get lead: %v", err)
	}
	loaded.Status = types.LeadStatusFollowing
	loaded.Version++
	loaded.UpdatedAt = now.Add(4 * time.Minute)
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("update lead: %v", err)
	}

	got, err := repo.GetByID(ctx, lead.ID)
	if err != nil {
		t.Fatalf("get updated lead: %v", err)
	}

	if got.Status != types.LeadStatusFollowing {
		t.Fatalf("status = %s, want %s", got.Status, types.LeadStatusFollowing)
	}
	if got.CustomerName != lead.CustomerName ||
		got.CustomerMobile != lead.CustomerMobile ||
		got.CustomerMobileMasked != lead.CustomerMobileMasked ||
		got.City != lead.City ||
		got.ProductName != lead.ProductName ||
		got.SourceChannel != lead.SourceChannel ||
		got.SourceCampaign != lead.SourceCampaign ||
		got.LeadLevel != lead.LeadLevel ||
		got.DispatchManagerUserID != lead.DispatchManagerUserID ||
		got.AssignedSalesUserID != lead.AssignedSalesUserID ||
		got.OwnerUserID != lead.OwnerUserID ||
		got.DealAmount != lead.DealAmount ||
		got.CloseReason != lead.CloseReason {
		t.Fatalf("unchanged scalar fields were not preserved: got=%#v want=%#v", got, lead)
	}
	if !sameOptionalTime(got.AssignedAt, lead.AssignedAt) ||
		!sameOptionalTime(got.ClaimedAt, lead.ClaimedAt) ||
		!sameOptionalTime(got.FirstFollowupAt, lead.FirstFollowupAt) {
		t.Fatalf("unchanged time fields were not preserved: got=%#v want=%#v", got, lead)
	}
}

func TestGormLeadRepositoryEncryptsCustomerContactsAtRest(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "leads-encrypted.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	protector, err := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new protector: %v", err)
	}
	hasher, err := security.NewHMACMobileHasher("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}

	repo := NewGormLeadRepository(db).WithFieldProtector(protector).WithMobileHasher(hasher)
	ctx := context.Background()
	now := time.Now()
	lead := &types.Lead{
		LeadNo:               "L-ENCRYPTED-1",
		CustomerName:         "加密客户",
		CustomerMobile:       "13912345678",
		CustomerMobileMasked: "139****5678",
		CustomerWechat:       "student-wechat-id",
		CustomerWechatMasked: "st****id",
		Status:               types.LeadStatusPendingDispatch,
		Version:              1,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := repo.Create(ctx, lead); err != nil {
		t.Fatalf("create lead: %v", err)
	}

	var stored LeadModel
	if err := db.WithContext(ctx).First(&stored, lead.ID).Error; err != nil {
		t.Fatalf("read stored model: %v", err)
	}
	if stored.CustomerMobile == "13912345678" || !security.IsEncryptedField(stored.CustomerMobile) {
		t.Fatalf("stored customer_mobile is not encrypted: %q", stored.CustomerMobile)
	}
	if stored.CustomerMobileHash == "" || stored.CustomerMobileHash == "13912345678" {
		t.Fatalf("stored customer_mobile_hash is invalid: %q", stored.CustomerMobileHash)
	}
	if stored.CustomerWechat == "student-wechat-id" || !security.IsEncryptedField(stored.CustomerWechat) {
		t.Fatalf("stored customer_wechat is not encrypted: %q", stored.CustomerWechat)
	}

	got, err := repo.GetByID(ctx, lead.ID)
	if err != nil {
		t.Fatalf("get lead: %v", err)
	}
	if got.CustomerMobile != "13912345678" {
		t.Fatalf("decrypted customer_mobile = %q", got.CustomerMobile)
	}
	if got.CustomerWechat != "student-wechat-id" {
		t.Fatalf("decrypted customer_wechat = %q", got.CustomerWechat)
	}
}

func TestGormLeadRepositoryListFilteredByKeyword(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "leads-search.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	protector, err := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new protector: %v", err)
	}
	hasher, err := security.NewHMACMobileHasher("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}

	repo := NewGormLeadRepository(db).WithFieldProtector(protector).WithMobileHasher(hasher)
	ctx := context.Background()
	now := time.Now()
	leads := []*types.Lead{
		{
			LeadNo:                "L-SEARCH-1",
			CustomerName:          "王小明",
			CustomerMobile:        "13912345678",
			CustomerMobileMasked:  "139****5678",
			City:                  "杭州",
			ProductName:           "初中数学",
			SourceChannel:         "ad:baidu",
			SourceCampaign:        "暑期计划",
			Status:                types.LeadStatusFollowing,
			Version:               1,
			DispatchManagerUserID: "manager1",
			AssignedSalesUserID:   "sales1",
			OwnerUserID:           "sales1",
			CreatedAt:             now,
			UpdatedAt:             now,
		},
		{
			LeadNo:                "L-SEARCH-2",
			CustomerName:          "张三",
			CustomerMobile:        "13800000000",
			CustomerMobileMasked:  "138****0000",
			City:                  "上海",
			ProductName:           "高中英语",
			SourceChannel:         "ad:xiaohongshu",
			SourceCampaign:        "品牌词100%",
			Status:                types.LeadStatusFollowing,
			Version:               1,
			DispatchManagerUserID: "manager1",
			AssignedSalesUserID:   "sales2",
			OwnerUserID:           "sales2",
			CreatedAt:             now,
			UpdatedAt:             now.Add(time.Minute),
		},
	}
	for _, lead := range leads {
		if err := repo.Create(ctx, lead); err != nil {
			t.Fatalf("create lead: %v", err)
		}
	}

	tests := []struct {
		name    string
		keyword string
		wantNo  string
	}{
		{name: "customer name", keyword: "小明", wantNo: "L-SEARCH-1"},
		{name: "full mobile hash", keyword: "13912345678", wantNo: "L-SEARCH-1"},
		{name: "full mobile with country code", keyword: "+86 13912345678", wantNo: "L-SEARCH-1"},
		{name: "masked mobile tail", keyword: "5678", wantNo: "L-SEARCH-1"},
		{name: "source channel", keyword: "xiaohongshu", wantNo: "L-SEARCH-2"},
		{name: "literal percent", keyword: "100%", wantNo: "L-SEARCH-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := repo.ListAllFiltered(ctx, LeadListFilter{
				Status:  string(types.LeadStatusFollowing),
				Keyword: tt.keyword,
			}, 0, 20)
			if err != nil {
				t.Fatalf("list filtered: %v", err)
			}
			if total != 1 || len(got) != 1 || got[0].LeadNo != tt.wantNo {
				t.Fatalf("filtered result total=%d leads=%+v, want %s", total, got, tt.wantNo)
			}
		})
	}

	got, total, err := repo.ListBySalesFiltered(ctx, "sales1", LeadListFilter{
		Status:  string(types.LeadStatusFollowing),
		Keyword: "英语",
	}, 0, 20)
	if err != nil {
		t.Fatalf("list sales filtered: %v", err)
	}
	if total != 0 || len(got) != 0 {
		t.Fatalf("sales scope leaked another user's lead: total=%d leads=%+v", total, got)
	}
}

func TestGormLeadRepositoryManagerSeesSharedPendingDispatchPool(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "leads-shared-pending.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewGormLeadRepository(db)
	ctx := context.Background()
	now := time.Now()
	leads := []*types.Lead{
		{
			LeadNo:                "L-SHARED-1",
			CustomerName:          "待分配一",
			Status:                types.LeadStatusPendingDispatch,
			Version:               1,
			DispatchManagerUserID: "manager1",
			CreatedAt:             now,
			UpdatedAt:             now,
		},
		{
			LeadNo:                "L-SHARED-2",
			CustomerName:          "待分配二",
			Status:                types.LeadStatusPendingDispatch,
			Version:               1,
			DispatchManagerUserID: "manager2",
			CreatedAt:             now.Add(time.Minute),
			UpdatedAt:             now.Add(time.Minute),
		},
		{
			LeadNo:                "L-OWNED-1",
			CustomerName:          "已分配",
			Status:                types.LeadStatusAssignedPendingClaim,
			Version:               1,
			DispatchManagerUserID: "manager1",
			AssignedSalesUserID:   "sales1",
			CreatedAt:             now,
			UpdatedAt:             now,
		},
	}
	for _, lead := range leads {
		if err := repo.Create(ctx, lead); err != nil {
			t.Fatalf("create lead: %v", err)
		}
	}

	got, total, err := repo.ListByManagerFiltered(ctx, "manager2", LeadListFilter{
		Status: string(types.LeadStatusPendingDispatch),
	}, 0, 20)
	if err != nil {
		t.Fatalf("list shared pending: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("shared pending total=%d len=%d, want 2", total, len(got))
	}

	got, total, err = repo.ListByManagerFiltered(ctx, "manager2", LeadListFilter{
		Status: string(types.LeadStatusAssignedPendingClaim),
	}, 0, 20)
	if err != nil {
		t.Fatalf("list owned pending claim: %v", err)
	}
	if total != 0 || len(got) != 0 {
		t.Fatalf("manager scope leaked assigned lead: total=%d leads=%+v", total, got)
	}
}

func TestGormLeadRepositoryDispatcherScopesUseOnlyCreationAndInitialAssignmentHistory(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "dispatcher-scope.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	leads := NewGormLeadRepository(db)
	dispatches := NewGormDispatchRecordRepository(db)
	ctx := context.Background()
	now := time.Now()
	items := []*types.Lead{
		{LeadNo: "DISPATCH-PENDING", CustomerName: "待分配", Status: types.LeadStatusPendingDispatch, CreatedByUserID: "someone", CreatedAt: now, UpdatedAt: now},
		{LeadNo: "DISPATCH-CREATED", CustomerName: "本人创建", Status: types.LeadStatusFollowing, CreatedByUserID: "dispatcher-1", CreatedByRole: types.UserRoleDispatcher, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)},
		{LeadNo: "DISPATCH-ASSIGNED", CustomerName: "本人分配", Status: types.LeadStatusFollowing, CreatedByUserID: "someone", CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute)},
		{LeadNo: "DISPATCH-REASSIGNED", CustomerName: "仅转派", Status: types.LeadStatusFollowing, CreatedByUserID: "someone", CreatedAt: now.Add(3 * time.Minute), UpdatedAt: now.Add(3 * time.Minute)},
		{LeadNo: "DISPATCH-OTHER", CustomerName: "他人分配", Status: types.LeadStatusFollowing, CreatedByUserID: "someone", CreatedAt: now.Add(4 * time.Minute), UpdatedAt: now.Add(4 * time.Minute)},
	}
	for _, lead := range items {
		if err := leads.Create(ctx, lead); err != nil {
			t.Fatalf("create %s: %v", lead.LeadNo, err)
		}
	}
	for _, record := range []*types.LeadDispatchRecord{
		{LeadID: items[2].ID, ManagerUserID: "dispatcher-1", SalesUserID: "inviter-1", ActionType: "assign", CreatedAt: now},
		{LeadID: items[3].ID, ManagerUserID: "dispatcher-1", SalesUserID: "inviter-2", ActionType: "reassign", CreatedAt: now},
		{LeadID: items[4].ID, ManagerUserID: "dispatcher-2", SalesUserID: "inviter-3", ActionType: "assign", CreatedAt: now},
	} {
		if err := dispatches.Create(ctx, record); err != nil {
			t.Fatalf("create dispatch record: %v", err)
		}
	}

	assertScope := func(scope types.DispatcherLeadScope, want ...string) {
		t.Helper()
		got, total, err := leads.ListByDispatcherFiltered(ctx, "dispatcher-1", scope, LeadListFilter{}, 0, 20)
		if err != nil {
			t.Fatalf("list scope %s: %v", scope, err)
		}
		if total != len(want) || len(got) != len(want) {
			t.Fatalf("scope %s total=%d len=%d, want %d: %+v", scope, total, len(got), len(want), got)
		}
		seen := make(map[string]bool, len(got))
		for _, lead := range got {
			seen[lead.LeadNo] = true
		}
		for _, leadNo := range want {
			if !seen[leadNo] {
				t.Fatalf("scope %s missing %s: %+v", scope, leadNo, seen)
			}
		}
	}

	assertScope(types.DispatcherLeadScopePending, "DISPATCH-PENDING")
	assertScope(types.DispatcherLeadScopeHandled, "DISPATCH-CREATED", "DISPATCH-ASSIGNED")
	assertScope(types.DispatcherLeadScopeAll, "DISPATCH-PENDING", "DISPATCH-CREATED", "DISPATCH-ASSIGNED")

	for _, tc := range []struct {
		leadID int64
		want   bool
	}{
		{items[0].ID, true},
		{items[1].ID, true},
		{items[2].ID, true},
		{items[3].ID, false},
		{items[4].ID, false},
	} {
		got, err := leads.CanDispatcherViewLead(ctx, tc.leadID, "dispatcher-1")
		if err != nil || got != tc.want {
			t.Fatalf("can view lead %d = %v, err=%v, want %v", tc.leadID, got, err, tc.want)
		}
	}
}

func TestGormLeadRepositoryAtomicAssignClaimsSharedPendingDispatchLead(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "leads-atomic-assign.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewGormLeadRepository(db)
	ctx := context.Background()
	now := time.Now()
	lead := &types.Lead{
		LeadNo:                "L-ATOMIC-ASSIGN-1",
		CustomerName:          "待抢分",
		Status:                types.LeadStatusPendingDispatch,
		Version:               1,
		DispatchManagerUserID: "manager1",
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := repo.Create(ctx, lead); err != nil {
		t.Fatalf("create lead: %v", err)
	}

	ok, err := repo.AtomicAssign(ctx, lead.ID, "manager2", "sales2", 1, lead.ProfileVersion)
	if err != nil {
		t.Fatalf("atomic assign: %v", err)
	}
	if !ok {
		t.Fatal("atomic assign returned false, want true")
	}
	ok, err = repo.AtomicAssign(ctx, lead.ID, "manager3", "sales3", 1, lead.ProfileVersion)
	if err != nil {
		t.Fatalf("second atomic assign: %v", err)
	}
	if ok {
		t.Fatal("second atomic assign returned true, want false")
	}

	got, err := repo.GetByID(ctx, lead.ID)
	if err != nil {
		t.Fatalf("get lead: %v", err)
	}
	if got.Status != types.LeadStatusAssignedPendingClaim ||
		got.DispatchManagerUserID != "manager2" ||
		got.AssignedSalesUserID != "sales2" ||
		got.AssignedInviterUserID != "sales2" ||
		got.OwnershipState != types.OwnershipAssignedPendingClaim ||
		got.Version != 2 {
		t.Fatalf("lead after atomic assign = %+v", got)
	}
}

func TestGormLeadRepositoryStatsUsesShanghaiNaturalDayBoundaries(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "stats-time.db"), MaxConns: 4})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewGormLeadRepository(db)
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	dayStart := time.Date(2026, 7, 31, 0, 0, 0, 0, location)
	times := []time.Time{
		dayStart.Add(-time.Nanosecond),
		dayStart,
		dayStart.Add(15*time.Hour + 59*time.Minute),
		dayStart.Add(16 * time.Hour),
		dayStart.Add(23*time.Hour + 59*time.Minute + 59*time.Second + 999999999*time.Nanosecond),
		dayStart.AddDate(0, 0, 1),
	}
	for hour := 1; hour <= 12; hour++ {
		times = append(times, dayStart.Add(time.Duration(hour)*time.Hour))
	}
	for i, createdAt := range times {
		lead := &types.Lead{
			LeadNo: "TIME-" + strconv.Itoa(i), CustomerName: "边界客户",
			Status: types.LeadStatusPendingDispatch, CreatedAt: createdAt, UpdatedAt: createdAt,
		}
		if err := repository.Create(context.Background(), lead); err != nil {
			t.Fatalf("create lead %d: %v", i, err)
		}
	}
	endExclusive := dayStart.AddDate(0, 0, 1)
	got, err := repository.ListForStats(context.Background(), LeadStatsFilter{StartTime: &dayStart, EndTime: &endExclusive}, 100)
	if err != nil {
		t.Fatalf("list stats: %v", err)
	}
	if len(got) != 16 {
		t.Fatalf("natural day count = %d, want 16", len(got))
	}
}

func TestGormLeadRepositoryStatsAndListShareManagerScope(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "manager-scope.db"), MaxConns: 4})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewGormLeadRepository(db)
	now := time.Now()
	leads := []*types.Lead{
		{LeadNo: "SCOPE-GLOBAL", Status: types.LeadStatusPendingDispatch, DispatchManagerUserID: "manager2", CreatedAt: now, UpdatedAt: now},
		{LeadNo: "SCOPE-OWN", Status: types.LeadStatusFollowing, DispatchManagerUserID: "manager1", CreatedAt: now, UpdatedAt: now},
		{LeadNo: "SCOPE-FRONTLINE", Status: types.LeadStatusFollowing, DispatchManagerUserID: "manager2", CreatedByRole: types.UserRoleInviter, CreatedAt: now, UpdatedAt: now},
		{LeadNo: "SCOPE-OTHER", Status: types.LeadStatusFollowing, DispatchManagerUserID: "manager2", CreatedByRole: types.UserRoleAdmin, CreatedAt: now, UpdatedAt: now},
	}
	for _, lead := range leads {
		if err := repository.Create(context.Background(), lead); err != nil {
			t.Fatalf("create %s: %v", lead.LeadNo, err)
		}
	}
	stats, err := repository.ListForStats(context.Background(), LeadStatsFilter{ManagerUserID: "manager1"}, 100)
	if err != nil {
		t.Fatalf("list stats: %v", err)
	}
	listed, total, err := repository.ListByManagerFiltered(context.Background(), "manager1", LeadListFilter{}, 0, 100)
	if err != nil {
		t.Fatalf("list leads: %v", err)
	}
	if len(stats) != 3 || len(listed) != 3 || total != 3 {
		t.Fatalf("scope mismatch: stats=%d listed=%d total=%d, want 3", len(stats), len(listed), total)
	}
}

func TestGormLeadRepositoryCloserScopeOnlyReturnsNegotiationOwnership(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver: "sqlite", Database: filepath.Join(t.TempDir(), "closer-scope.db"), MaxConns: 1,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewGormLeadRepository(db)
	now := time.Now()
	for _, item := range []*types.Lead{
		{LeadNo: "NEG-1", Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageNegotiation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, CloserUserID: "closer-1", CurrentOwnerUserID: "closer-1", CurrentOwnerRole: types.OwnerRoleCloser, CreatedAt: now, UpdatedAt: now},
		{LeadNo: "INV-1", Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, CloserUserID: "closer-1", CreatedAt: now, UpdatedAt: now},
		{LeadNo: "NEG-2", Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageNegotiation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, CloserUserID: "closer-2", CreatedAt: now, UpdatedAt: now},
	} {
		if err := repository.Create(context.Background(), item); err != nil {
			t.Fatalf("create %s: %v", item.LeadNo, err)
		}
	}
	leads, total, err := repository.ListByCloserFiltered(context.Background(), "closer-1", LeadListFilter{}, 0, 20)
	if err != nil || total != 1 || len(leads) != 1 || leads[0].LeadNo != "NEG-1" {
		t.Fatalf("closer scope total=%d leads=%+v err=%v", total, leads, err)
	}
}

func sameOptionalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func TestCurrentFollowupSQLMatchesDomainEligibility(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repository := NewGormLeadRepository(db)
	now := time.Now()
	due := now.Add(-time.Hour)
	for i, change := range []func(*types.Lead){func(*types.Lead) {}, func(l *types.Lead) { l.Status = types.LeadStatusInvalid }, func(l *types.Lead) { l.OwnershipState = types.OwnershipPublicPool }, func(l *types.Lead) { l.OwnershipState = types.OwnershipAssignedPendingClaim }, func(l *types.Lead) { id := int64(3); l.CurrentAppointmentID = &id }, func(l *types.Lead) { l.CurrentOwnerUserID = ""; l.OwnerUserID = "" }, func(l *types.Lead) { l.BusinessStage = types.BusinessStageNegotiation }} {
		lead := &types.Lead{LeadNo: "qualification-" + strconv.Itoa(i), CustomerName: "test", Status: types.LeadStatusFollowing, ResultStatus: types.LeadResultActive, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate, CurrentOwnerUserID: "owner", OwnerUserID: "owner", NextFollowupDueAt: &due, CreatedAt: now, UpdatedAt: now}
		change(lead)
		if err := repository.Create(t.Context(), lead); err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Model(&LeadModel{}).Where(currentFollowupPredicate).Where("id = ?", lead.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if (count == 1) != types.IsCurrentFollowupEligible(lead) {
			t.Fatalf("predicate mismatch %d", i)
		}
	}
	snapshot, err := repository.BuildWorkbenchLeadSnapshot(t.Context(), "manager", types.UserRoleManager, now, 20)
	if err != nil || snapshot.OverdueFollowup != 2 {
		t.Fatalf("summary=%+v error=%v", snapshot, err)
	}
}
