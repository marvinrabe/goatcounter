select * from paths
where
	site = :site
	{{if .after}}and path_id > :after{{end}}
order by path, path_id
{{if .limit}}limit :limit{{end}}
