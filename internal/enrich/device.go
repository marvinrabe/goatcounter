package enrich

// The device category: phone, tablet, desktop, or unknown. Unlike the other
// dimensions it's not stored, but computed when querying: collected rows have
// the screen width, and rows migrated from Plausible its device name.
const (
	// DeviceFromWidth is the category of the width column, in CSS pixels.
	DeviceFromWidth = `case
	when width = 0    then 'unknown'
	when width <= 600  then 'phone'
	when width <= 1000 then 'tablet'
	else 'desktop' end`

	// DeviceFromPlausible is the category of the device column.
	DeviceFromPlausible = `case device
	when 'Mobile'  then 'phone'
	when 'Tablet'  then 'tablet'
	when 'Desktop' then 'desktop'
	when 'Laptop'  then 'desktop'
	else 'unknown' end`
)
