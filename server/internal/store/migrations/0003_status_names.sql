-- These established department/workflow names are identical in every language.
UPDATE statuses SET label_en = CASE lower(value)
 WHEN 'feasibility' THEN 'Feasibility' WHEN 'devel' THEN 'Devel' WHEN 'sales' THEN 'Sales' END,
 label_cs = CASE lower(value)
 WHEN 'feasibility' THEN 'Feasibility' WHEN 'devel' THEN 'Devel' WHEN 'sales' THEN 'Sales' END,
 label_it = CASE lower(value)
 WHEN 'feasibility' THEN 'Feasibility' WHEN 'devel' THEN 'Devel' WHEN 'sales' THEN 'Sales' END
WHERE lower(value) IN ('feasibility','devel','sales');
