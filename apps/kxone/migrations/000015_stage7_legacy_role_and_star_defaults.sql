-- Finalize the approved legacy defaults before enabling workflow V2.
-- Keep legacy columns and account identities unchanged.

UPDATE crm_wecom_user_bindings
SET role = 'inviter', updated_at = CURRENT_TIMESTAMP
WHERE role = 'sales';

UPDATE lead_followups
SET star_rating = CASE intention_level
  WHEN 'high' THEN 5
  WHEN 'medium' THEN 3
  WHEN 'low' THEN 1
  ELSE 3
END
WHERE star_rating = 0;

-- +kxone dialect sqlite
UPDATE leads
SET star_rating = COALESCE((
  SELECT lead_followups.star_rating
  FROM lead_followups
  WHERE lead_followups.lead_id = leads.id AND lead_followups.star_rating BETWEEN 1 AND 5
  ORDER BY lead_followups.created_at DESC, lead_followups.id DESC
  LIMIT 1
), 3)
WHERE star_rating = 0;

-- +kxone dialect mysql
UPDATE leads l
LEFT JOIN (
  SELECT f.lead_id, f.star_rating
  FROM lead_followups f
  JOIN (
    SELECT lead_id, MAX(id) AS latest_id
    FROM lead_followups
    WHERE star_rating BETWEEN 1 AND 5
    GROUP BY lead_id
  ) latest ON latest.latest_id = f.id
) mapped ON mapped.lead_id = l.id
SET l.star_rating = COALESCE(mapped.star_rating, 3)
WHERE l.star_rating = 0;
