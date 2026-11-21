export const user = {
  id: 1,
  username: 'admin',
  employee_name: '员工丙',
  wecom_userid: 'fixture-admin',
  role: 'admin',
  features: {
    workflow_v2_enabled: true,
    appointment_enabled: true,
    pool_enabled: true,
    pool_auto_return_enabled: false
  }
}

export const leads = [
  {
    id: 28,
    lead_no: 'KX202608270028',
    customer_name: '张若溪',
    customer_mobile_masked: '138****5621',
    customer_wechat_masked: 'wx_zh***xi',
    city: '湖北省襄阳市樊城区',
    product_name: '高三艺考文化课全日制冲刺班',
    source_channel: '小红书',
    status: 'following',
    business_stage: 'invitation',
    current_owner_name: '员工己',
    current_owner_userid: 'fixture-inviter',
    assigned_inviter_name: '员工己',
    assigned_inviter_userid: 'fixture-inviter',
    created_by_name: '系统接入',
    star_rating: 4,
    version: 3,
    profile_version: 1,
    created_at: '2026-08-27T08:30:00+08:00',
    updated_at: '2026-08-27T10:20:00+08:00',
    next_followup_due_at: '2026-08-27T14:00:00+08:00'
  },
  {
    id: 29,
    lead_no: 'KX202608270029',
    customer_name: '李女士',
    customer_mobile_masked: '186****1029',
    city: '襄州区',
    product_name: '复读提分方案',
    source_channel: '抖音',
    status: 'pending_dispatch',
    business_stage: 'invitation',
    version: 0,
    profile_version: 1,
    created_at: '2026-08-27T09:10:00+08:00',
    updated_at: '2026-08-27T09:10:00+08:00'
  }
]

const dashboard = {
  overview: { conversion_rate: 0.23, total_deal_amount: 126800, average_deal_amount: 8453, overdue_dispatch_count: 2 },
  assignment: { total: 38, assigned: 31, pending_dispatch: 4, pending_claim: 3, claim_rate: 0.87, first_followup_rate: 0.79, overdue_claim_count: 1, overdue_followup_count: 3 },
  dual_stage: { invitation_rate: 0.68, arrival_rate: 0.54, negotiation_conversion_rate: 0.31, schedule_utilization_rate: 0.72, pool_turnover_rate: 0.26, current_followup_timeliness_rate: 0.81 },
  funnel: [{ name: '新线索', count: 38 }, { name: '持续跟进', count: 19 }, { name: '已成交', count: 6 }],
  sources: [{ source: '小红书', account: '襄阳校区', total: 22, first_followups: 17, invalid: 2, conversion_rate: 0.27 }],
  sales: [{ sales_userid: 'fixture-inviter', sales_name: '员工己', assigned: 18, pending_claim: 2, claimed: 16, following: 12, claim_rate: 0.89 }],
  managers: [{ manager_userid: 'fixture-admin', manager_name: '员工丙', total: 38, pending_dispatch: 4, conversion_rate: 0.23 }],
  followup: { total_records: 83, avg_records_per_lead: 2.2, due_today_count: 8, overdue_count: 3, by_result: [{ name: 'connected', count: 52 }] },
  next_tasks: [{ lead_id: 28, lead_no: 'KX202608270028', customer_name: '张若溪', sales_name: '员工己', next_followup_at: '2026-08-27T14:00:00+08:00', overdue: false }]
}

export const managementStats = {
 date_basis: 'lead_created_at', generated_at: '2026-08-27T10:30:00+08:00',
 assignment: { total: 38, assigned: 31, pending_dispatch: 4, pending_claim: 3, converted: 6, invalid: 2 },
 teams: [{ role: 'inviter', sales_userid: 'fixture-inviter', sales_name: '员工己', assigned: 18, converted: 4, pending_claim: 2, first_followups: 15, first_followup_rate: 0.8333, conversion_rate: 0.2222, overdue_followup_count: 2, unknown_first_followup: 0 }],
 sources: [{ source: '小红书', account: '襄阳校区', total: 22, converted: 6, first_followups: 17, invalid: 2, conversion_rate: 6/22, invalid_rate: 2/22 }]
}
export const followupSupervision = {
 generated_at: '2026-08-27T10:30:00+08:00', summary: { pending_dispatch: 4, pending_claim: 3, overdue_followup: 3, overdue_first_followup: 1, unknown_first_followup: 1, missing_deadline: 1 },
 owners: [{ userid: 'fixture-inviter', name: '员工己', overdue_followup: 3, overdue_first_followup: 1, pending_claim: 3, unknown_first_followup: 1, missing_deadline: 1 }],
 tasks: [{ type: 'overdue_followup', lead_id: 28, title: '张若溪 · 跟进逾期', owner_userid: 'fixture-inviter', owner_name: '员工己', due_at: '2026-08-27T09:00:00+08:00' }]
}

const appointments = [{ id: 3, lead_id: 28, lead_no: 'KX202608270028', customer_name: '张若溪', start_at: '2026-08-27T14:00:00+08:00', end_at: '2026-08-27T15:30:00+08:00', campus: '襄阳校区', visitor_count: 2, inviter_name: '员工己', closer_name: '赵老师', status: 'scheduled', version: 1 }]
export const poolLeads = [{ ...leads[1], id: 30, lead_no: 'KX202608260030', customer_name: '周同学', status: 'public_pool', pool_reason: 'followup_overdue', pool_entered_at: '2026-08-27T07:20:00+08:00', star_rating: 3 }]

export const json = (route, body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

export async function mockAPI(page) {
  await page.clock.setFixedTime(new Date('2026-08-27T10:30:00+08:00'))
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/api/, '')
    if (path === '/user/info') return json(route, user)
    if (path === '/agent/conversations') return json(route, { conversations: [], total: 0 })
    if (path === '/agent/settings') return json(route, { enabled: true, configured: true, key_configured: true, base_url: 'https://provider.example/v1', model: 'qwen3.8-max', budget_micro: 300e6, spent_micro: 0, reserved_micro: 0, input_price_cny: 12, output_price_cny: 36, version: 1 })
    if (path === '/agent/preferences') return json(route, { content: '' })
    if (path === '/agent/rules') return json(route, { rules: [] })
    if (path === '/leads') return json(route, { leads, total: leads.length, page: 1, page_size: 20, total_pages: 1 })
    if (path === '/leads/28') return json(route, { lead: leads[0], followups: [{ id: 1, sales_name: '员工己', remark: '家长已了解课程，明天确认到访时间。', followup_result: 'connected', star_rating: 4, created_at: '2026-08-27T10:20:00+08:00' }], dispatch_records: [], activities: [] })
    if (path === '/workbench') return json(route, { summary: { pending_dispatch: 4, pending_claim: 3, pending_appointment_assignment: 2, overdue_followup: 3 }, tasks: leads })
    if (path === '/users/lead-owner-filters' || path === '/users/inviters') return json(route, { bindings: [{ wecom_userid: 'fixture-inviter', employee_name: '员工己', role: 'inviter', status: 'active' }] })
    if (path === '/users/closers') return json(route, { bindings: [{ wecom_userid: 'fixture-closer', employee_name: '赵老师', role: 'closer', status: 'active' }] })
    if (path === '/me/lead-list-preferences') return json(route, { version: 1, columns: [], density: 'comfortable' })
    if (path === '/me/lead-views') return json(route, { views: [] })
    if (path === '/stats/dashboard') return json(route, dashboard)
    if (path === '/stats/management') return json(route, managementStats)
    if (path === '/stats/supervision') return json(route, followupSupervision)
    if (path === '/appointments') return json(route, { appointments })
    if (path === '/closer-availability') return json(route, { availability: [] })
    if (path === '/public-pool') return json(route, { leads: poolLeads, total: 1, total_pages: 1 })
    if (path === '/admin/settings') return json(route, { settings: [] })
    if (path === '/admin/users') return json(route, { bindings: [user], total: 1, total_pages: 1 })
    if (path === '/channel-integrations') return json(route, { integrations: [], public_base_url: 'https://crm.example.test' })
    return json(route, {})
  })
}
