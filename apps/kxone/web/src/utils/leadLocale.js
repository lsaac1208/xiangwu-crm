const followupResultLabels = {
  connected: '已联系',
  no_answer: '未接通',
  interested: '有意向',
  not_interested: '无意向',
  invalid: '无效线索',
  again: '需要再次跟进'
}

const operationSourceLabels = {
  wecom_card: '企业微信卡片',
  h5: 'CRM 页面',
  api: '系统接口',
  manual: '人工操作'
}

export const followupResultLabel = (value) => {
  const code = String(value || '').trim()
  return followupResultLabels[code] || code || '-'
}

export const operationSourceLabel = (value) => {
  const code = String(value || '').trim()
  return operationSourceLabels[code] || code || '-'
}

export const localizeSystemRemark = (value) => {
  let text = String(value || '').trim()
  Object.entries(operationSourceLabels).forEach(([code, label]) => {
    text = text.replace(new RegExp(`\\b${code}\\b`, 'g'), label)
  })
  return text
}
