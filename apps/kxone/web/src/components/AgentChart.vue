<template><div ref="element" class="agent-chart" role="img" :aria-label="label"></div></template>
<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { init, use } from 'echarts/core'
import { BarChart, LineChart, PieChart, ScatterChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent, AriaComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
use([BarChart, LineChart, PieChart, ScatterChart, GridComponent, TooltipComponent, LegendComponent, AriaComponent, CanvasRenderer])
const props = defineProps({ option: { type: Object, required: true }, label: { type: String, required: true } })
const element = ref(null)
let chart, observer
const render = async () => {
  await nextTick()
  if (!element.value) return
  if (!chart) {
    chart = init(element.value)
    if (typeof ResizeObserver !== 'undefined') { observer = new ResizeObserver(() => chart?.resize()); observer.observe(element.value) }
  }
  chart.setOption(props.option, true)
}
watch(() => props.option, render)
onMounted(render)
onBeforeUnmount(() => { observer?.disconnect(); chart?.dispose() })
</script>
<style scoped>
.agent-chart { width: 100%; height: 300px; min-width: 0; }
@media (max-width: 767px) { .agent-chart { height: 240px; } }
</style>
