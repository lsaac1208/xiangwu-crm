package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/lsaac128/kxone/internal/security"
	"golang.org/x/text/unicode/norm"
)

type snapshotProtectorKey struct{}
type snapshotCredentialsKey struct{}

// The configured credential is a local exclusion value, never a snapshot
// column. This also covers opaque keys without a recognizable secret prefix.
func withSnapshotCredentials(ctx context.Context, values ...string) context.Context {
	return context.WithValue(ctx, snapshotCredentialsKey{}, append([]string{}, values...))
}

// WithSnapshotFieldProtector enables local matching of encrypted contacts. The
// protector and its key never enter the snapshot, tool descriptions, or logs.
func WithSnapshotFieldProtector(ctx context.Context, protector security.FieldProtector) context.Context {
	return context.WithValue(ctx, snapshotProtectorKey{}, protector)
}

type redactionEntry struct {
	value string
	ref   string
}

type redactor struct {
	credentials         []string
	names               map[string]string
	contacts            map[string]string
	employees           map[string]bool
	employeeIdentifiers map[string]string
	ordered             []redactionEntry
	contactIDs          []redactionEntry
	nameReplacer        *strings.Replacer
	tokenNames          []redactionEntry
	contactReplacer     *strings.Replacer
	trustedDimension    bool
	businessReplacer    *strings.Replacer
	businessRestore     *strings.Replacer
}

var (
	phonePattern            = regexp.MustCompile(`(?:\+?86[\s.\-]*)?(?:1[3-9](?:[\s.\-]*[0-9]){9}|0[0-9]{2,3}[\s.\-()]*[0-9](?:[\s.\-]*[0-9]){6,7})(?:[\s.\-]*(?:转|ext\.?)[\s.]*[0-9]{1,6})?`)
	shortPhonePattern       = regexp.MustCompile(`(?:电话|手机|座机|联系方式|tel|phone|mobile)\s*[:：=]?\s*[+()0-9\s.\-]{6,30}`)
	wechatPattern           = regexp.MustCompile(`(?i)(?:微信(?:号)?|微\s*信|薇信|v信|vx|wechat|weixin|wx)\s*[:：=]?\s*[a-zA-Z0-9_\-@.]{3,64}`)
	emailPattern            = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9\-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9\-]*[a-z0-9])?)+`)
	idCardPattern           = regexp.MustCompile(`\b[1-9][0-9]{16}[0-9Xx]\b`)
	secretPattern           = regexp.MustCompile(`(?i)(?:enc:v1:[a-z0-9+/=_\-]+|(?:sk-|Bearer\s+)[a-z0-9._\-]{12,}|(?:password|passwd|api[_-]?key|secret|token|密码|密钥)\s*[:：=]\s*[^\s,，;；}]{3,})`)
	unknownNamePattern      = regexp.MustCompile(`(?:客户|家长|学生|孩子|姓名|联系人|父亲|母亲|爸爸|妈妈|父母|父|母)\s*[:：=]?\s*([\p{Han}]{2,5}|[A-Za-z][A-Za-z .'-]{1,40})`)
	explicitNamePattern     = regexp.MustCompile(`(?:(?:客户|家长|学生|孩子|联系人|父亲|母亲|爸爸|妈妈|父母)(?:姓名)?|(?:^|[\s，。;；:："“”‘'（）()、])姓名)\s*[:：=]\s*(客户#[0-9]+(?:、客户#[0-9]+)*|[\p{Han}]{2,5}|[A-Za-z][A-Za-z .'-]{1,40})`)
	familyTitlePattern      = regexp.MustCompile(`[赵钱孙李周吴郑王冯陈蒋沈韩杨朱秦许何吕张孔曹严华金魏陶姜谢邹苏潘葛范彭鲁韦马任方鲍史唐费薛雷贺倪汤罗郝安常傅余顾孟黄萧尹姚邵汪毛戴宋庞熊纪舒董梁杜阮蓝闵季贾江郭梅盛林钟徐邱骆高夏蔡田胡凌霍万卢莫房解丁邓洪崔程陆翁龚袁邢裴石段侯白廖曾谭刘卢叶][\p{Han}]{0,3}(?:爸爸|妈妈|家长|先生|女士|同学|小朋友)`)
	credentialURLPattern    = regexp.MustCompile(`(?i)(?:https?://[^\s"<>]+\?(?:[^\s"<>]*&)?(?:token|key|secret|password)=[^\s"<>]+)`)
	standaloneNamePattern   = regexp.MustCompile(`(?:^|[\s，。;；:："“”‘'（）()、]|看看|查询|关于|查一下|分析|查|问|找)([赵钱孙李周吴郑王冯陈蒋沈韩杨朱秦许何吕张孔曹严华金魏陶姜谢邹苏潘葛范彭鲁韦马任方鲍史唐费薛雷贺倪汤罗郝安常傅余顾孟黄萧尹姚邵汪毛戴宋庞熊纪舒董梁杜阮蓝闵季贾江郭梅盛林钟徐邱骆高夏蔡田胡凌霍万卢莫房解丁邓洪崔程陆翁龚袁邢裴石段侯白廖曾谭刘卢叶][\p{Han}]{1,3})(?:$|[\s，。;；:："“”‘'（）()、]|的|家长)`)
	encodedPhonePattern     = regexp.MustCompile(`(?i)(?:电话|手机|座机|联系方式|tel|phone|mobile)\s*[:：=]?\s*(?:[零〇一二三四五六七八九十幺\s\-]{7,30}|(?:base64[:：\s]*)?[A-Za-z0-9+/=_\-]{10,64})`)
	proseNamePattern        = regexp.MustCompile(`[赵钱孙李周吴郑王冯陈蒋沈韩杨朱秦许何吕张孔曹严华金魏陶姜谢邹苏潘葛范彭鲁韦马任方鲍史唐费薛雷贺倪汤罗郝安常傅余顾孟黄萧尹姚邵汪毛戴宋庞熊纪舒董梁杜阮蓝闵季贾江郭梅盛林钟徐邱骆高夏蔡田胡凌霍万卢莫房解丁邓洪崔程陆翁龚袁邢裴石段侯白廖曾谭刘卢叶][\p{Han}]{1,3}(?:同意|表示|说|希望|要求|愿意|需要|考虑|想|已|今天|昨天|明天|到访|来校|会|的)`)
	degreeSurnamePattern    = regexp.MustCompile(`(?:比较|非常|特别|相对|较|很|更|最|太|偏|过|挺|不)[` + surnameChars + `]`)
	concernsBusinessPattern = regexp.MustCompile(`(?:客户|家长)顾虑(?:$|[\s，,。.；;：:、？?！!]|是否|和费用|与费用)`)
	parenthesesShield       = strings.NewReplacer("（", "\ue000", "）", "\ue001")
	parenthesesRestore      = strings.NewReplacer("\ue000", "（", "\ue001", "）")
)

func newRedactor() *redactor {
	r := &redactor{names: make(map[string]string), contacts: make(map[string]string), employees: make(map[string]bool), employeeIdentifiers: make(map[string]string)}
	// Business vocabulary that happens to start with a surname is not itself a
	// person. Known actual customer names still take precedence in prepare().
	for _, value := range strings.Fields("高一 高二 高三 高中 高考 初一 初二 初三 初中 中考 大专 本科 中专 职高 复读 应届 往届 周一 周二 周三 周四 周五 周六 周日 周天 金额 单价 费用 收费 学费 金融 方差 陈述 结果 总额 净额 平均 总计 合计 校区 到访 跟进 负责人 邀约 公海 成交 退款 付款 回收站 历史 范围 方法") {
		r.employees[value] = true
	}
	return r
}

func normalizedSensitiveText(text string) string {
	// Decode escaped JSON and HTML representations before matching; normalize
	// full-width digits and remove invisible separators used to evade matching.
	for i := 0; i < 2; i++ {
		text = html.UnescapeString(text)
		if strings.Contains(text, "%") {
			// PathUnescape preserves literal plus signs in phone numbers.
			if decoded, err := url.PathUnescape(text); err == nil {
				text = decoded
			}
		}
		if strings.Contains(text, `\u`) {
			text = regexp.MustCompile(`\\u[0-9a-fA-F]{4}`).ReplaceAllStringFunc(text, func(v string) string {
				var s string
				if json.Unmarshal([]byte(`"`+v+`"`), &s) == nil {
					return s
				}
				return v
			})
		}
	}
	// Preserve the punctuation of existing source labels while normalizing
	// full-width contact digits. Full-width parentheses are business data.
	text = parenthesesShield.Replace(text)
	text = norm.NFKC.String(text)
	text = parenthesesRestore.Replace(text)
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) || r == '\x00' {
			return -1
		}
		return r
	}, text)
}

func (r *redactor) addName(value, ref string) {
	value = strings.TrimSpace(normalizedSensitiveText(value))
	if value == "" {
		return
	}
	if old, ok := r.names[value]; ok && old != ref {
		r.names[value] = mergeCustomerRefs(old, ref)
	} else {
		r.names[value] = ref
	}
}

func (r *redactor) addContact(value, ref string) {
	value = strings.TrimSpace(normalizedSensitiveText(value))
	if value == "" || security.IsEncryptedField(value) || !meaningfulContact(value) {
		return
	}
	r.contacts[value] = mergeCustomerRefs(r.contacts[value], ref)
	compact := strings.Map(func(ch rune) rune {
		if unicode.IsSpace(ch) || strings.ContainsRune("+().-", ch) {
			return -1
		}
		return ch
	}, value)
	if len(compact) >= 6 {
		r.contacts[compact] = mergeCustomerRefs(r.contacts[compact], ref)
	}
}

// meaningfulContact rejects placeholder or fragment values ("*", "无", a
// three-digit stub, masked "138****") that cannot identify anyone but would
// match ordinary text, dates or tool arguments as substrings.
func meaningfulContact(value string) bool {
	alnum := 0
	for _, ch := range value {
		if ch == '*' || ch == '＊' {
			return false
		}
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			alnum++
		}
	}
	return alnum >= minContactChars
}

const minContactChars = 6

func mergeCustomerRefs(old, ref string) string {
	if old == "" {
		return ref
	}
	refs := strings.Split(old, "、")
	for _, known := range refs {
		if known == ref {
			return old
		}
	}
	if len(refs) < 50 {
		refs = append(refs, ref)
		sort.Strings(refs)
		return strings.Join(refs, "、")
	}
	if !strings.Contains(old, "[更多同名线索需核对]") {
		return old + "、[更多同名线索需核对]"
	}
	return old
}

func (r *redactor) prepare() {
	r.ordered = nil
	r.contactIDs = nil
	for value, ref := range r.names {
		r.ordered = append(r.ordered, redactionEntry{value, ref})
	}
	for value, ref := range r.contacts {
		r.contactIDs = append(r.contactIDs, redactionEntry{value, ref})
	}
	sort.Slice(r.ordered, func(i, j int) bool { return len(r.ordered[i].value) > len(r.ordered[j].value) })
	sort.Slice(r.contactIDs, func(i, j int) bool { return len(r.contactIDs[i].value) > len(r.contactIDs[j].value) })
	nameArgs := make([]string, 0, len(r.ordered)*2)
	r.tokenNames = nil
	r.nameReplacer = nil
	for _, entry := range r.ordered {
		if substringSafeName(entry.value) {
			nameArgs = append(nameArgs, entry.value, entry.ref)
		} else {
			// Short or non-Han names ("20", "李", "Amy") only match as a
			// standalone token so dates, times and numbers stay intact.
			r.tokenNames = append(r.tokenNames, entry)
		}
	}
	if len(nameArgs) > 0 {
		r.nameReplacer = strings.NewReplacer(nameArgs...)
	}
	contactArgs := make([]string, 0, len(r.contactIDs)*2)
	for _, entry := range r.contactIDs {
		contactArgs = append(contactArgs, entry.value, entry.ref+"[联系方式已脱敏]")
	}
	if len(contactArgs) > 0 {
		r.contactReplacer = strings.NewReplacer(contactArgs...)
	}
	// Shield known, complete business dimensions before name heuristics. The
	// replacement restores only the contact/secret-cleaned value; it cannot
	// reintroduce a phone number or credential into a tool or prompt.
	var business []string
	for value := range r.employees {
		if value != "" {
			customerCollision := false
			for _, name := range r.ordered {
				// A business-word prefix must not hide a registered full name
				// from the subsequent name replacement (including old aliases).
				if strings.Contains(name.value, value) {
					customerCollision = true
					break
				}
			}
			if !customerCollision {
				business = append(business, value)
			}
		}
	}
	for value := range r.employeeIdentifiers {
		business = append(business, value)
	}
	sort.Slice(business, func(i, j int) bool { return len(business[i]) > len(business[j]) })
	var shieldArgs, restoreArgs []string
	for i, value := range business {
		marker := fmt.Sprintf("【业务维度%d】", i)
		copy := *r
		copy.nameReplacer = nil
		copy.tokenNames = nil
		copy.trustedDimension = true
		copy.businessReplacer = nil
		copy.businessRestore = nil
		clean, _ := copy.sanitize(value)
		if employeeRef, ok := r.employeeIdentifiers[value]; ok {
			clean = employeeRef
		}
		shieldArgs = append(shieldArgs, value, marker)
		restoreArgs = append(restoreArgs, marker, clean)
	}
	if len(shieldArgs) > 0 {
		r.businessReplacer = strings.NewReplacer(shieldArgs...)
		r.businessRestore = strings.NewReplacer(restoreArgs...)
	}
}

func (r *redactor) sanitize(text string) (string, int) {
	text = normalizedSensitiveText(text)
	if len(text) > 65536 {
		return "[超长片段未纳入分析]", 1
	}
	excluded := 0
	for _, value := range r.credentials {
		value = normalizedSensitiveText(value)
		if value != "" && strings.Contains(text, value) {
			excluded += strings.Count(text, value)
			text = strings.ReplaceAll(text, value, "[凭证片段未纳入分析]")
		}
	}
	if !r.trustedDimension {
		// An explicit customer/parent identity annotation is stronger evidence
		// than a coincident business word or employee name. Keep established
		// customer bindings, but never shield an unknown annotated identity.
		text = explicitNamePattern.ReplaceAllStringFunc(text, func(value string) string {
			parts := explicitNamePattern.FindStringSubmatch(value)
			name := parts[1]
			if strings.HasPrefix(name, "客户#") {
				return value
			}
			if ref, known := r.names[name]; known {
				return strings.TrimSuffix(value, name) + ref
			}
			excluded++
			return strings.TrimSuffix(value, name) + "[身份片段未纳入分析]"
		})
	}
	// Protect this ambiguous surname word only inside an established business
	// phrase with a lexical boundary. A prefix such as 顾虑安 remains subject
	// to identity guards, and known customer names always take precedence.
	const concernsShield = "\ue020\ue021"
	if !r.trustedDimension {
		text = concernsBusinessPattern.ReplaceAllStringFunc(text, func(value string) string {
			for name := range r.names {
				if strings.Contains(name, "顾虑") {
					return value
				}
			}
			return strings.Replace(value, "顾虑", concernsShield, 1)
		})
	}
	// A surname character right after a degree adverb is an adjective, not
	// the start of a name: “费用较高的说法” was flagged as 高+的+说 (v29 A21-2
	// diagnosis). Known customer names were already replaced above.
	if !r.trustedDimension {
		text = degreeSurnamePattern.ReplaceAllStringFunc(text, shieldDegreeSurname)
	}
	if r.businessReplacer != nil && !r.trustedDimension {
		text = r.businessReplacer.Replace(text)
	}
	if r.nameReplacer != nil {
		text = r.nameReplacer.Replace(text)
	}
	for _, entry := range r.tokenNames {
		text = replaceToken(text, entry.value, entry.ref)
	}
	if r.contactReplacer != nil {
		text = r.contactReplacer.Replace(text)
	}
	text = phonePattern.ReplaceAllStringFunc(text, func(s string) string {
		compact := strings.Map(func(ch rune) rune {
			if unicode.IsSpace(ch) || strings.ContainsRune("+().-", ch) {
				return -1
			}
			return ch
		}, s)
		if ref, ok := r.contacts[compact]; ok {
			return ref + "[联系方式已脱敏]"
		}
		return "[电话已脱敏]"
	})
	text = shortPhonePattern.ReplaceAllString(text, "[电话已脱敏]")
	text = encodedPhonePattern.ReplaceAllString(text, "[电话片段已脱敏]")
	text = wechatPattern.ReplaceAllString(text, "[微信已脱敏]")
	text = emailPattern.ReplaceAllString(text, "[邮箱已脱敏]")
	text = idCardPattern.ReplaceAllString(text, "[证件已脱敏]")
	for _, pattern := range []*regexp.Regexp{secretPattern, credentialURLPattern} {
		text = pattern.ReplaceAllStringFunc(text, func(string) string { excluded++; return "[敏感片段未纳入分析]" })
	}
	if r.trustedDimension {
		return text, excluded
	}
	// This exact, generated marker contains “方式已”, which otherwise looks
	// like a Fang-surname sentence. Shield only the fixed marker, after contact
	// and secret checks; arbitrary bracketed user text remains untrusted.
	const contactMarkerShield = " \ue010\ue011 "
	text = strings.ReplaceAll(text, "[联系方式已脱敏]", contactMarkerShield)
	text = unknownNamePattern.ReplaceAllStringFunc(text, func(value string) string {
		// A replaced customer reference is safe. Generic descriptions such as
		// “家长表示” are not assumed to be real names.
		if strings.Contains(value, "客户#") || strings.Contains(value, "客户[同名]") {
			return value
		}
		name := unknownNamePattern.FindStringSubmatch(value)
		if len(name) < 2 {
			return value
		}
		if r.employees[strings.TrimSpace(name[1])] {
			return value
		}
		if strings.ContainsAny(value, ":：=") || appearsName(name[1]) {
			excluded++
			return "[身份片段未纳入分析]"
		}
		return value
	})
	text = familyTitlePattern.ReplaceAllStringFunc(text, func(value string) string { excluded++; return "[家长称谓已脱敏]" })
	text = proseNamePattern.ReplaceAllStringFunc(text, func(value string) string { excluded++; return "[身份片段未纳入分析]" })
	text = standaloneNamePattern.ReplaceAllStringFunc(text, func(value string) string {
		parts := standaloneNamePattern.FindStringSubmatch(value)
		if len(parts) > 1 && !r.employees[parts[1]] {
			excluded++
			return strings.Replace(value, parts[1], "[身份片段未纳入分析]", 1)
		}
		return value
	})
	text = strings.ReplaceAll(text, contactMarkerShield, "[联系方式已脱敏]")
	if r.businessRestore != nil {
		text = r.businessRestore.Replace(text)
	}
	text = strings.ReplaceAll(text, concernsShield, "顾虑")
	text = restoreDegreeSurname(text)
	return text, excluded
}

func appearsName(value string) bool {
	first, _, ok := strings.Cut(value, " ")
	if ok {
		value = first
	}
	runes := []rune(value)
	if len(runes) < 2 || len(runes) > 5 {
		return false
	}
	return strings.ContainsRune("赵钱孙李周吴郑王冯陈蒋沈韩杨朱秦许何吕张孔曹严华金魏陶姜谢邹苏潘葛范彭鲁韦马任方鲍史唐费薛雷贺倪汤罗郝安常傅余顾孟黄萧尹姚邵汪毛戴宋庞熊纪舒董梁杜阮蓝闵季贾江郭梅盛林钟徐邱骆高夏蔡田胡凌霍万卢莫房解丁邓洪崔程陆翁龚袁邢裴石段侯白廖曾谭刘卢叶", runes[0])
}

// JSON is projected with an explicit business-key allowlist. New or unknown
// fields are excluded until reviewed, rather than sent through as raw payloads.
func (r *redactor) sanitizeJSON(value string, leadID int64) (string, int) {
	if strings.TrimSpace(value) == "" {
		return "", 0
	}
	var payload any
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	if decoder.Decode(&payload) != nil {
		return "[不可解析历史片段未纳入分析]", 1
	}
	count := 0
	var walk func(any, string, int) any
	walk = func(v any, key string, depth int) any {
		if depth > 12 {
			count++
			return "[嵌套片段未纳入分析]"
		}
		switch t := v.(type) {
		case map[string]any:
			out := make(map[string]any)
			for k, item := range t {
				canonical := strings.ToLower(k)
				if isContactJSONKey(canonical) {
					out[k] = "[联系方式已脱敏]"
					continue
				}
				if isNameJSONKey(canonical) {
					out[k] = fmt.Sprintf("客户#%d", leadID)
					continue
				}
				if !allowedBusinessJSONKey(canonical) {
					count++
					continue
				}
				out[k] = walk(item, canonical, depth+1)
			}
			return out
		case []any:
			out := make([]any, 0, len(t))
			for _, item := range t {
				out = append(out, walk(item, key, depth+1))
			}
			return out
		case string:
			clean, n := r.sanitize(t)
			count += n
			return clean
		default:
			return v
		}
	}
	data, _ := json.Marshal(walk(payload, "", 0))
	return string(data), count
}

func isContactJSONKey(k string) bool {
	for _, fragment := range []string{"mobile", "phone", "wechat", "weixin", "contact", "email", "address", "微信", "电话", "手机", "住址", "身份证"} {
		if strings.Contains(k, fragment) {
			return true
		}
	}
	return false
}

func isNameJSONKey(k string) bool {
	return k == "customer_name" || k == "name" || k == "student_name" || k == "parent_name" || k == "customer_alias" || k == "alias" || k == "姓名"
}

func allowedBusinessJSONKey(k string) bool {
	if strings.Contains(k, "secret") || strings.Contains(k, "token") || strings.Contains(k, "password") || strings.Contains(k, "hash") || strings.Contains(k, "payload") {
		return false
	}
	allowed := " id lead_id appointment_id appointment_no lead_no status from_status to_status business_stage ownership_state result_status star_rating lead_level version profile_version pool_version business_version action event_type activity_type reason remark customer_concerns handoff_notes special_notes cancelled_reason no_show_reason rescheduled_reason source_channel source_campaign city product_name campaign payment_status deal_amount refund_amount conversion_path changed_fields batch before after evidence rule evidence_ids plan_key from to field previous_closer_userid business_timezone campus visitor_count start_at end_at occurred_at created_at updated_at assigned_at claimed_at first_followup_at current_owner_started_at current_owner_first_followup_at last_followup_at next_followup_due_at inviter_assigned_at inviter_claimed_at arrived_at delivered_at pool_entered_at converted_at closed_at inviter_userid closer_userid owner_userid current_owner_userid current_owner_role assigned_inviter_userid assigned_sales_userid dispatch_manager_userid manager_userid sales_userid actor_userid actor_role assigned_by_userid created_by_userid created_by_role previous_owner_userid account_name platform callback_path auth_mode enabled last_status last_received_at last_lead_id capacity kind created_by updated_by trace_id repair_key source_ids first_followup_event_id "
	return strings.Contains(allowed, " "+k+" ")
}

const surnameChars = "赵钱孙李周吴郑王冯陈蒋沈韩杨朱秦许何吕张孔曹严华金魏陶姜谢邹苏潘葛范彭鲁韦马任方鲍史唐费薛雷贺倪汤罗郝安常傅余顾孟黄萧尹姚邵汪毛戴宋庞熊纪舒董梁杜阮蓝闵季贾江郭梅盛林钟徐邱骆高夏蔡田胡凌霍万卢莫房解丁邓洪崔程陆翁龚袁邢裴石段侯白廖曾谭刘卢叶"

// Private-use runes stand in for the shielded surname character so no name
// pattern can start at it; restored after all identity patterns ran.
const degreeShieldBase = 0xF0000

func shieldDegreeSurname(value string) string {
	runes := []rune(value)
	last := runes[len(runes)-1]
	index := strings.IndexRune(surnameChars, last)
	if index < 0 {
		return value
	}
	runes[len(runes)-1] = rune(degreeShieldBase + len([]rune(surnameChars[:index])))
	return string(runes)
}

func restoreDegreeSurname(text string) string {
	surnames := []rune(surnameChars)
	return strings.Map(func(r rune) rune {
		if r >= degreeShieldBase && r < degreeShieldBase+rune(len(surnames)) {
			return surnames[r-degreeShieldBase]
		}
		return r
	}, text)
}

// substringSafeName reports whether a customer name is specific enough to be
// replaced wherever it appears: at least two Han characters, or at least
// minContactChars letters/digits. Shorter values ("20", "李", "Amy") are only
// replaced as standalone tokens so dates, times and numbers stay intact.
func substringSafeName(value string) bool {
	han, alnum := 0, 0
	for _, ch := range value {
		if unicode.Is(unicode.Han, ch) {
			han++
		}
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			alnum++
		}
	}
	return han >= 2 || alnum >= minContactChars
}

func asciiWordRune(ch rune) bool {
	return ch < 128 && (unicode.IsLetter(ch) || unicode.IsDigit(ch) || strings.ContainsRune("-:./_", ch))
}

// replaceToken replaces value only where it is not glued to a neighbouring
// character of the same kind: ASCII values need non-ASCII-word neighbours (so
// "20" never matches inside "2026-10-14"); Han values need non-letter
// neighbours.
func replaceToken(text, value, ref string) string {
	if value == "" || !strings.Contains(text, value) {
		return text
	}
	ascii := true
	for _, ch := range value {
		if ch >= 128 {
			ascii = false
		}
	}
	digits := strings.Trim(value, "0123456789") == ""
	glued := func(ch rune) bool {
		if digits && strings.ContainsRune("年月日号时点分秒周天个条单笔元万千百岁人次位％%", ch) {
			// "10月14日", "共20条": a short number is a quantity, not a name.
			return true
		}
		if ascii {
			return asciiWordRune(ch)
		}
		return unicode.IsLetter(ch) || unicode.IsDigit(ch)
	}
	var b strings.Builder
	rest := text
	for {
		i := strings.Index(rest, value)
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		before := []rune(rest[:i])
		after := []rune(rest[i+len(value):])
		ok := (len(before) == 0 || !glued(before[len(before)-1])) && (len(after) == 0 || !glued(after[0]))
		if digits && strings.HasSuffix(b.String()+rest[:i], "【业务维度") {
			// Never touch the index of a temporary business-dimension shield.
			ok = false
		}
		b.WriteString(rest[:i])
		if ok {
			b.WriteString(ref)
		} else {
			b.WriteString(value)
		}
		rest = rest[i+len(value):]
	}
}
