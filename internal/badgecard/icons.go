package badgecard

// Icons are the badge line icons (24×24 SVG elements drawn with a stroke, no fill), keyed by
// service.BadgeDef.Icon. The profile page puts them in an <svg> (template func badgeIcon); Render
// draws them into the share image. Only path (M L H V C A Z, absolute or relative), circle and rect
// elements are used, which is all draw.go understands.
var Icons = map[string]string{
	"merge":    `<circle cx="6" cy="5" r="2.5"/><circle cx="6" cy="19" r="2.5"/><circle cx="18" cy="12" r="2.5"/><path d="M6 7.5v9M6 9c0 3 3 3 9.5 3"/>`,
	"upload":   `<path d="M12 15V4M7.5 8.5L12 4l4.5 4.5"/><path d="M4 14v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4"/>`,
	"flame":    `<path d="M12 3c1 3.5 5 5.5 5 10a5 5 0 0 1-10 0c0-2.5 1.5-4 2.5-5 .3 1.6 1 2.5 2 3 0-3 .5-5.5.5-8z"/>`,
	"week":     `<rect x="3.5" y="5" width="17" height="15" rx="2"/><path d="M3.5 10h17M8 3v4M16 3v4M7 14h10"/>`,
	"month":    `<rect x="3.5" y="5" width="17" height="15" rx="2"/><path d="M3.5 10h17M8 3v4M16 3v4M8 14h2M14 14h2M8 17h2M14 17h2"/>`,
	"calendar": `<rect x="3.5" y="5" width="17" height="15" rx="2"/><path d="M3.5 10h17M8 3v4M16 3v4M9 15l2 2 4-4"/>`,
	"pen":      `<path d="M4 20l4-1 11-11-3-3L5 16z"/><path d="M14 6l3 3"/>`,
	"type":     `<path d="M5 19l5-14h1l5 14M7 14h7"/><path d="M17 19h3"/>`,
	"check":    `<circle cx="12" cy="12" r="8.5"/><path d="M8 12.5l3 3 5-6"/>`,
	"polish":   `<path d="M12 3l1.6 4.4L18 9l-4.4 1.6L12 15l-1.6-4.4L6 9l4.4-1.6z"/><path d="M18 15l.8 2.2L21 18l-2.2.8L18 21l-.8-2.2L15 18l2.2-.8z"/>`,
	"bolt":     `<path d="M13 3L5 13h6l-1 8 8-10h-6z"/>`,
	"flag":     `<path d="M5 21V4M5 4h11l-2 4 2 4H5"/>`,
	"layers":   `<path d="M12 4l9 5-9 5-9-5z"/><path d="M3 14l9 5 9-5"/>`,
	"grid":     `<rect x="4" y="4" width="7" height="7" rx="1"/><rect x="13" y="4" width="7" height="7" rx="1"/><rect x="4" y="13" width="7" height="7" rx="1"/><rect x="13" y="13" width="7" height="7" rx="1"/>`,
	"globe":    `<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c2.5 2.5 3.5 5.5 3.5 8.5s-1 6-3.5 8.5c-2.5-2.5-3.5-5.5-3.5-8.5s1-6 3.5-8.5z"/>`,
	"chat":     `<path d="M4 5h16v11H9l-5 4z"/><path d="M8 9h8M8 12h5"/>`,
	"topic":    `<path d="M4 5h16v11H9l-5 4z"/><path d="M12 8v5M9.5 10.5h5"/>`,
	"book":     `<path d="M5 3h12a2 2 0 0 1 2 2v16H7a2 2 0 0 1-2-2z"/><path d="M5 17a2 2 0 0 1 2-2h12M9 7h6M9 10h4"/>`,
	"note":     `<path d="M9 3h6l-1 6 3 3H7l3-3z"/><path d="M12 12v9"/>`,
	"commit":   `<circle cx="12" cy="12" r="3.5"/><path d="M3 12h5.5M15.5 12H21"/>`,
	"moon":     `<path d="M19 14.5A7.5 7.5 0 1 1 9.5 5a6 6 0 0 0 9.5 9.5z"/>`,
	"sun":      `<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2.5M12 19v2.5M2.5 12H5M19 12h2.5M5.3 5.3l1.8 1.8M16.9 16.9l1.8 1.8M5.3 18.7l1.8-1.8M16.9 7.1l1.8-1.8"/>`,
	"weekend":  `<path d="M4 13V9a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v4"/><path d="M3 13h18v4H3zM5 17v2M19 17v2"/>`,
	"medal":    `<circle cx="12" cy="14" r="5.5"/><path d="M8.5 9.5L6 3h4l2 4 2-4h4l-2.5 6.5"/><path d="M12 11.5l.9 1.8 2 .3-1.4 1.4.3 2-1.8-.9-1.8.9.3-2-1.4-1.4 2-.3z"/>`,
}
