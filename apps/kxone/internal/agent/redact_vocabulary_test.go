package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func TestRedactorHistoricalBusinessWordsDoNotInventIdentityExclusions(t *testing.T) {
	r := newRedactor()
	r.prepare()
	for _, question := range []string{
		"以线索当前业务状态判断普通回访资格，哪些线索的当前截止时间早于上海时间 2026-10-08 10:00？请说明这不等于还原该历史时刻的负责人和业务状态。",
		"历史记录中的负责人不等于当前负责人。",
		"请核对历史阶段与当前阶段，不能把历史事实改成当前状态。",
	} {
		clean, excluded := r.sanitize(question)
		if clean != normalizedSensitiveText(question) || excluded != 0 {
			t.Errorf("business time/state language was treated as identity: %q -> %q; exclusions=%d", question, clean, excluded)
		}
	}
}

func TestRedactorHistoricalVocabularyStillProtectsShiNamesAndKnownAliases(t *testing.T) {
	r := newRedactor()
	r.prepare()
	for _, text := range []string{"史小明", "史建军说费用需要商量", "历史记录中，史大勇的家长希望到访", "史时刻的家长希望下周沟通"} {
		clean, excluded := r.sanitize(text)
		if excluded == 0 {
			t.Errorf("possible customer name was not excluded: %q -> %q", text, clean)
		}
		for _, name := range []string{"史小明", "史建军", "史大勇", "史时刻"} {
			if strings.Contains(clean, name) {
				t.Errorf("Shi surname exception exposed customer name: %q -> %q", text, clean)
			}
		}
	}

	// Even an exact business-vocabulary collision remains a customer alias
	// once historical business data explicitly identifies it as a name.
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	if err := db.Table("leads").Where("id=1").Update("customer_name", "史小明").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repo.LeadActivityRecordModel{ID: 1, LeadID: 1, ActivityType: "customer_profile_updated", FromValueJSON: `{"customer_name":"历史"}`, ToValueJSON: `{"customer_name":"史小明"}`, CreatedAt: time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)}).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	for _, alias := range []string{"史小明", "历史"} {
		clean, _ := snapshot.Sanitize("查询" + alias + "的跟进")
		if strings.Contains(clean, alias) || !strings.Contains(clean, "客户#1") {
			t.Errorf("known customer/alias lost privacy precedence: %q -> %q", alias, clean)
		}
	}
}

func TestRedactorKnownNameContainingBusinessWordKeepsPrivacyPrecedence(t *testing.T) {
	r := newRedactor()
	r.addName("历史时刻", "客户#99")
	r.prepare()
	clean, _ := r.sanitize("查询历史时刻的跟进")
	if strings.Contains(clean, "历史时刻") || !strings.Contains(clean, "客户#99") {
		t.Fatalf("shielding a business-word prefix hid a known full customer name: %q", clean)
	}
}

func TestRedactorGeneratedContactMarkerIsIdempotentWithoutShieldingUserData(t *testing.T) {
	r := newRedactor()
	r.addContact("13912345678", "客户#1")
	r.prepare()
	first, exclusions := r.sanitize("电话 13912345678")
	if first != "电话 客户#1[联系方式已脱敏]" || exclusions != 0 {
		t.Fatalf("generated contact marker was reinterpreted as a customer name: %q, %d", first, exclusions)
	}
	second, exclusions := r.sanitize(first)
	if second != first || exclusions != 0 {
		t.Fatalf("repeated sanitization changed its own marker: %q -> %q (%d)", first, second, exclusions)
	}
	for _, text := range []string{"[联系方式已脱敏]史小明", "[联系方式已脱敏:13912345678]", "[联系人:史大勇]", "[联系方式已脱敏] 13800000000"} {
		clean, _ := r.sanitize(text)
		for _, sensitive := range []string{"史小明", "史大勇", "13912345678", "13800000000"} {
			if strings.Contains(clean, sensitive) {
				t.Errorf("fixed-marker handling shielded arbitrary user data: %q -> %q", text, clean)
			}
		}
	}
}
