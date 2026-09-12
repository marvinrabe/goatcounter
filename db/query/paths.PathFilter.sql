select path_id from paths
where
	site = :site
	{{:invert and not ( 1=1}}
		{{:only_event    and event=1}}
		{{:only_pageview and event=0}}
		{{:have_like and lower(path) :not like lower(:like)}}
	{{:invert )}}
