package agent

func buildSourceDeals(p MetricParams) ([]metricQuery, error) {
	f, err := leadFilters(p, "l.converted_at_date_shanghai", "l.current_owner_user_id")
	if err != nil {
		return nil, err
	}
	f.add(convertedSQL)
	return []metricQuery{{sql: `SELECT COALESCE(NULLIF(l.source_channel,''),'未填写') AS "来源",COUNT(*) AS "成交数",` + amountYuanSQL("l.deal_amount_cents") + ` AS "成交金额(元)",` + netYuanSQL + ` AS "净成交额(元)" FROM leads l WHERE ` + f.clause() + ` GROUP BY 1 ORDER BY 2 DESC,1`, args: f.args, chart: &metricChart{"bar", "来源", []string{"成交数"}}}}, nil
}

func buildOwnerFirstResponse(p MetricParams) ([]metricQuery, error) {
	f, err := leadFilters(p, "l.current_owner_first_followup_at_date_shanghai", "l.current_owner_user_id")
	if err != nil {
		return nil, err
	}
	f.add(`COALESCE(l.current_owner_user_id,'')<>''`)
	return []metricQuery{{sql: `SELECT ` + uniqueOwnerNameSQL + ` AS "当前负责人",COUNT(*) AS "线索数",COALESCE(SUM(l.owner_first_followup_seconds IS NOT NULL),0) AS "负责人已首跟",COALESCE(SUM(l.current_owner_started_at IS NOT NULL AND l.current_owner_first_followup_at IS NULL),0) AS "尚未首跟",COALESCE(SUM(l.current_owner_started_at IS NULL OR (l.current_owner_first_followup_at IS NOT NULL AND l.owner_first_followup_seconds IS NULL)),0) AS "责任周期未知",ROUND(AVG(l.owner_first_followup_seconds)/60.0,1) AS "负责人平均首跟(分钟)" FROM leads l WHERE ` + f.clause() + ` GROUP BY l.current_owner_user_id ORDER BY 2 DESC,1`, args: f.args}}, nil
}
