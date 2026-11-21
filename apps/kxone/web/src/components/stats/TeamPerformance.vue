<template>
  <section class="report-team" aria-labelledby="team-heading">
    <div class="report-section-label"><span>01</span><h2 id="team-heading">每个人的进展</h2></div>
    <div class="report-team-controls">
      <div class="report-role-tabs" role="group" aria-label="人员归属角色"><button type="button" :aria-pressed="role === 'inviter'" @click="$emit('role', 'inviter')">邀约归属</button><button type="button" :aria-pressed="role === 'closer'" @click="$emit('role', 'closer')">谈单归属</button></div>
      <div class="report-sort report-brief-sort" role="group" aria-label="团队排序"><button v-for="item in sorts" :key="item.key" type="button" :aria-pressed="sortKey === item.key" @click="$emit('sort', item.key)">{{ item.label }}<span aria-hidden="true"> ↓</span></button></div>
    </div>
    <p class="report-comparison-note">按当前留存的{{ role === 'inviter' ? '邀约' : '谈单' }}归属比较线索结果；角色切换只影响人员对比。首跟率按可判断的责任记录计算。</p>
    <div class="report-people-cards">
      <button v-for="person in rows" :key="person.sales_userid" type="button" :disabled="!canSelect(person)" :title="canSelect(person) ? '查看这位人员参与的线索' : '历史成员，当前不在可筛选人员中'" :aria-pressed="selected === person.sales_userid" @click="$emit('select', person.sales_userid)">
        <span class="report-people-name"><strong>{{ person.sales_name || person.sales_userid }}</strong><small>{{ person.assigned }} 条线索<span v-if="person.assigned < 20"> · 小样本</span></small></span>
        <strong class="report-people-result">{{ person.converted }}<small> 成交</small></strong>
        <span class="report-people-meta">转化率 {{ percent(person.conversion_rate) }}</span>
        <span class="report-bar-label">首跟率 {{ percent(person.first_followup_rate) }}</span>
        <span class="report-mini-track" aria-hidden="true"><i :style="{ width: `${Math.max(0, Math.min(1, person.first_followup_rate || 0)) * 100}%` }"></i></span>
        <small>{{ person.overdue_followup_count }} 条跟进逾期 · {{ person.pending_claim }} 条待领</small>
        <small v-if="person.unknown_first_followup" class="report-record-note">{{ person.unknown_first_followup }} 条首跟记录不可判断</small>
      </button>
    </div>
    <p v-if="!rows.length" class="report-empty">当前范围暂无可比较的{{ role === 'inviter' ? '邀约' : '谈单' }}数据，可调整日期或样本筛选。</p>
    <p class="report-comparison-note">转化率 = 归属线索中的成交数 / 归属线索数。邀约、谈单可能参与同一条线索，两组不相加。</p>
  </section>
</template>
<script setup>
const props = defineProps({ rows: { type: Array, default: () => [] }, role: { type: String, required: true }, sortKey: { type: String, required: true }, allowed: { type: Array, default: () => [] }, selected: { type: String, default: '' } })
defineEmits(['role', 'sort', 'select'])
const sorts = [{ key: 'converted', label: '成交数' }, { key: 'conversion_rate', label: '转化率' }, { key: 'assigned', label: '线索量' }, { key: 'overdue_followup_count', label: '跟进逾期' }]
const canSelect = person => props.allowed.some(item => item.wecom_userid === person.sales_userid)
const percent = value => value == null ? '—' : `${(value * 100).toFixed(1)}%`
</script>
