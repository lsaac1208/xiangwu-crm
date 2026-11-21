package agent

import (
	"strings"
	"testing"
)

func TestBusinessCustomerConcernsAreNotInventedIdentities(t *testing.T) {
	r := newRedactor()
	r.prepare()
	for _, text := range []string{
		"再查阅这些线索的跟进记录，概括客户顾虑，附可打开的记录依据。",
		"归纳家长顾虑和费用问题，不把当前负责人当作历史操作人。",
		"客户顾虑是否涉及费用？记录能否证明流失原因？",
	} {
		clean, count := r.sanitize(text)
		if clean != normalizedSensitiveText(text) || count != 0 {
			t.Errorf("business question damaged: %q => %q (%d)", text, clean, count)
		}
	}
}

func TestConcernsVocabularyDoesNotHideCustomerIdentity(t *testing.T) {
	r := newRedactor()
	r.prepare()
	for _, text := range []string{"客户姓名：顾虑", "家长：顾虑", "客户顾小安的备注", "顾建军说想来校", "客户顾虑安的备注", "顾虑安说想来校"} {
		clean, count := r.sanitize(text)
		if count == 0 || strings.Contains(clean, "顾虑") || strings.Contains(clean, "顾小安") || strings.Contains(clean, "顾建军") || strings.Contains(clean, "顾虑安") {
			t.Errorf("identity escaped: %q => %q", text, clean)
		}
	}
	for _, name := range []string{"顾虑", "顾虑安"} {
		r := newRedactor()
		r.addName(name, "客户#17")
		r.prepare()
		clean, _ := r.sanitize("概括客户" + name + "的记录")
		if strings.Contains(clean, name) || !strings.Contains(clean, "客户#17") {
			t.Errorf("known name lost precedence: %q => %q", name, clean)
		}
	}
}
