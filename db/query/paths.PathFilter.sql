select path_id from paths
where
	site = :site
	{{if .invert}}and not ( 1=1{{end}}
		{{if .only_event}}and event=1{{end}}
		{{if .only_pageview}}and event=0{{end}}
		{{if .have_like}}and lower(path) :not like lower(:like){{end}}
	{{if .invert}}){{end}}
