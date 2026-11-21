<template>
  <section class="report-panel report-channels">
    <header class="report-panel-head"><div><span class="report-eyebrow">来源与账户</span><h2>渠道效果 <small>{{ rows.length }} 个来源</small></h2></div><span class="report-muted">量、跟进、转化一起看</span></header>
    <div class="report-channel-toggles" role="group" aria-label="渠道比较指标"><button v-for="item in metrics" :key="item.key" type="button" :aria-pressed="metric === item.key" @click="metric = item.key">{{ item.label }}</button></div>
    <div class="report-table-wrap" tabindex="0" role="region" aria-label="渠道明细">
      <table class="report-table report-channel-table"><thead><tr><th scope="col">来源 / 账户</th><th scope="col">线索</th><th scope="col">首跟率</th><th scope="col">成交</th><th scope="col">转化率</th><th scope="col">无效率</th></tr></thead><tbody>
        <tr v-for="source in sorted" :key="sourceKey(source)">
          <th scope="row"><button type="button" class="report-link" :aria-expanded="selected === sourceKey(source)" @click="select(source)">{{ source.source }}</button><small>{{ source.account || '未区分账户' }}</small></th>
          <td>{{ source.total }}<small v-if="source.total < 20" class="report-sample">小样本</small></td><td>{{ ratio(source.first_followups, source.total) }}</td><td>{{ source.converted }}</td><td><strong>{{ ratio(source.converted, source.total) }}</strong></td><td>{{ ratio(source.invalid, source.total) }}</td>
        </tr>
      </tbody></table>
    </div>
    <div v-if="selectedSource" class="report-source-detail" aria-live="polite"><button type="button" class="report-detail-close" aria-label="收起来源详情" @click="selected = ''">×</button><strong>{{ selectedSource.source }}{{ selectedSource.account ? ` · ${selectedSource.account}` : '' }} · {{ selectedSource.total }} 条线索</strong><p>已首跟 {{ selectedSource.first_followups }} 条（{{ ratio(selectedSource.first_followups, selectedSource.total) }}），成交 {{ selectedSource.converted }} 条（{{ ratio(selectedSource.converted, selectedSource.total) }}），无效 {{ selectedSource.invalid }} 条（{{ ratio(selectedSource.invalid, selectedSource.total) }}）。</p><small>{{ selectedSource.total < 20 ? '样本较少，继续观察；暂不据此判断渠道优劣。' : '结合线索数量、跟进情况和实际投放成本判断。' }}</small></div>
    <p v-if="!rows.length" class="report-empty">当前范围暂无来源数据。</p>
    <footer class="report-panel-foot">转化率 = 成交 / 该来源线索。点来源查看首跟及具体数量；暂无投放成本数据。</footer>
  </section>
</template>
<script setup>
import { computed, ref } from 'vue'
const props = defineProps({ rows: { type: Array, default: () => [] } })
const metric = ref('total'), selected = ref('')
const metrics = [{ key: 'total', label: '线索量' }, { key: 'conversion_rate', label: '转化率' }, { key: 'invalid_rate', label: '无效率' }]
const sourceKey = source => JSON.stringify([source.source, source.account || ''])
const sorted = computed(() => [...props.rows].sort((a,b) => b[metric.value] - a[metric.value] || b.total - a.total || sourceKey(a).localeCompare(sourceKey(b))))
const selectedSource = computed(() => props.rows.find(source => sourceKey(source) === selected.value))
const ratio = (value,total) => total ? `${(value / total * 100).toFixed(1)}%` : '—'
const select = source => { selected.value = selected.value === sourceKey(source) ? '' : sourceKey(source) }
</script>
