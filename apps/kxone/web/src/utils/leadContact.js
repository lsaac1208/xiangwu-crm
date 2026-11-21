const maskText = (value, prefixLength, suffixLength) => {
  const text = String(value || '').trim()
  if (!text) return ''
  if (text.length <= prefixLength + suffixLength) return '*'.repeat(text.length)
  const hiddenLength = Math.min(Math.max(text.length - prefixLength - suffixLength, 4), 8)
  return `${text.slice(0, prefixLength)}${'*'.repeat(hiddenLength)}${text.slice(-suffixLength)}`
}

const safeServerMask = (value, prefixLength, suffixLength) => {
  const text = String(value || '').trim()
  if (!text) return ''
  const match = text.match(/^([^*]*)(\*{2,})([^*]*)$/)
  if (match && match[1].length <= prefixLength && match[3].length <= suffixLength) return text
  return maskText(text, prefixLength, suffixLength)
}

export const displayLeadMobile = (lead, forceMasked = false) => {
  if (!lead) return '未留电话'
  if (forceMasked) {
    return maskText(lead.customer_mobile, 3, 4) || safeServerMask(lead.customer_mobile_masked, 3, 4) || '未留电话'
  }
  return lead.customer_mobile || lead.customer_mobile_masked || '未留电话'
}

export const displayLeadWechat = (lead, forceMasked = false) => {
  if (!lead) return '未留微信'
  if (forceMasked) {
    return maskText(lead.customer_wechat, 2, 2) || safeServerMask(lead.customer_wechat_masked, 2, 2) || '未留微信'
  }
  return lead.customer_wechat || lead.customer_wechat_masked || '未留微信'
}
