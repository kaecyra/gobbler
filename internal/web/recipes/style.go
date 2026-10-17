package recipes

// libraryStyle is the styling of the home and library pages, from the
// mockup's #library section (docs/ui/mockup.html). The shared stylesheet
// belongs to the layout and holds the tokens, .btn, .seg, .search, .panel and
// .empty; this is what the two pages add. Colours, radii and the tap size come
// from the tokens, and every control is at least --tap tall.
const libraryStyle = `
.mtype { display: inline-flex; align-items: center; gap: 6px; line-height: 1; font-size: 13px; letter-spacing: .08em; text-transform: uppercase; font-weight: 650; color: var(--ink-3); }
.mtype svg { width: 1.2em; height: 1.2em; color: var(--walnut); display: block; flex: none; }
.heat { display: inline-flex; align-items: center; gap: 1px; color: var(--wattle); }
.heat svg { width: 16px; height: 16px; display: block; flex: none; }
.spoons { display: inline-flex; align-items: center; gap: 2px; }
.spoon { width: 16px; height: 16px; display: block; flex: none; }
.spoon ellipse { fill: var(--sunk); stroke: var(--slate); stroke-width: 1.3; }
.spoon path { stroke: var(--slate); fill: none; stroke-width: 2.4; stroke-linecap: round; }
.spoon.on ellipse { fill: var(--grain); stroke: var(--grain-ink); }
.spoon.on path { stroke: var(--grain); }

.hero { display: flex; flex-direction: column; align-items: center; text-align: center; gap: 16px; padding-block: 18px 40px; margin: 0; }
.hero h1 { font-size: clamp(28px, 4.4vw, 40px); font-weight: 700; }
.hero p { margin: 0; color: var(--ink-2); font-size: 16px; }
.hero .search { width: 100%; max-width: 680px; padding: 6px 10px 6px 20px; border-radius: var(--r); }
.hero .search input { font-size: 20px; padding: 14px 0; min-height: var(--tap); }
.hero.compact { align-items: stretch; text-align: left; padding-block: 0 22px; }
.hero.compact .search { max-width: 640px; }
.search kbd { min-height: 0; }

.lib { display: grid; grid-template-columns: minmax(0, 1fr) 280px; gap: 32px; align-items: start; }
@media (max-width: 900px) { .lib { grid-template-columns: minmax(0, 1fr); } }
.lib.home { display: block; }
.res-h { font-size: 15px; color: var(--ink-3); margin: 0; font-weight: 500; }
.res-h b { color: var(--ink); }
.grpby-row { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 16px; margin-bottom: 18px; }
.grpby { display: flex; align-items: center; gap: 10px; }
.grpby .seg > a { min-height: var(--tap); }
.groups { display: flex; flex-direction: column; gap: 30px; }
.grp-h { display: flex; align-items: center; gap: 9px; margin-bottom: 12px; font-size: 19px; font-weight: 700; line-height: 1; }
.grp-h svg { width: 1.15em; height: 1.15em; color: var(--walnut); display: block; flex: none; }
.grp-h small { font-family: var(--f-body); font-size: 13px; font-weight: 500; color: var(--ink-3); margin-left: 2px; }
.grp-h::after { content: ""; flex: 1; height: 1px; background: var(--line); margin-left: 6px; }

.rcard { position: relative; overflow: hidden; height: 100%; background: var(--card); border: 1px solid var(--line); box-shadow: var(--shadow); border-radius: var(--r); padding: 16px 16px 0; display: flex; flex-direction: column; gap: 10px; transition: transform .15s, border-color .15s; text-align: left; }
.rcard:hover, .rcard:focus-within { transform: translateY(-2px); border-color: var(--ink-3); }
.ctop { display: flex; justify-content: space-between; align-items: center; gap: 8px; min-height: 20px; }
.rcard h3 { font-size: 20px; font-weight: 650; padding-top: 4px; }
.rcard h3 a { display: flex; align-items: center; min-height: var(--tap); color: inherit; text-decoration: none; }
.rcard h3 a::after { content: ""; position: absolute; inset: 0; }
.rcard .src { font-size: 12.5px; color: var(--ink-3); margin-top: 2px; }
.comps { display: flex; flex-wrap: wrap; gap: 4px 10px; font-size: 14px; color: var(--ink-2); margin: 0; padding: 0; list-style: none; }
.comps li::before { content: ""; display: inline-block; width: 5px; height: 5px; border-radius: var(--r-sm); background: var(--slate); margin-right: 6px; vertical-align: 2px; }
.key { margin: 0; font-size: 14.5px; color: var(--ink-2); line-height: 1.45; }
.key em { font-style: normal; color: var(--ink); }
.rfoot { display: flex; justify-content: space-between; align-items: center; gap: 8px; font-size: 12.5px; color: var(--ink-3); padding-top: 4px; margin-top: auto; }
.rcard .rib { margin: 4px -16px 0; }

.side { display: flex; flex-direction: column; gap: 28px; }
.lib.home .side { display: grid; grid-template-columns: minmax(0, 1.55fr) minmax(280px, 1fr); gap: 24px; max-width: 1040px; margin: 34px auto 0; align-items: start; }
@media (max-width: 900px) { .lib.home .side { grid-template-columns: minmax(0, 1fr); max-width: 620px; } }
.lib.home .side .panel { border: 0; box-shadow: var(--shadow); padding: 20px 22px; }
.suggest { display: grid; grid-template-columns: 150px minmax(0, 1fr); position: relative; background: var(--card); border-radius: var(--r); box-shadow: var(--shadow); overflow: visible; }
.sg-gob { position: relative; }
.sg-gob img { position: absolute; left: 12px; bottom: 0; height: clamp(140px, calc(100% + 26px), 178px); width: auto; display: block; transform: scaleX(-1); }
.sg-body { display: flex; flex-direction: column; gap: 8px; padding: 22px 22px 0 6px; min-width: 0; }
.sg-voice { margin: 0; font-family: var(--f-display); font-size: 16px; font-style: italic; color: var(--ink-2); }
.sg-name { font-size: clamp(24px, 2.6vw, 30px); font-weight: 700; line-height: 1.12; }
.sg-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 16px; font-size: 14.5px; color: var(--ink-3); }
.sg-act { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 18px; margin-top: 6px; }
.sg-next { display: inline-flex; align-items: center; min-height: var(--tap); font-size: 15px; font-weight: 600; color: var(--wattle-ink); text-underline-offset: 3px; }
.sg-rib { margin: 12px -22px 0 0; }
.sg-rib .rib-seg:last-child { border-right-width: 0; }
@media (max-width: 520px) { .suggest { grid-template-columns: 112px minmax(0, 1fr); } .sg-gob img { left: 6px; height: clamp(120px, calc(100% + 22px), 146px); } }

.lib:not(.home) .suggest { grid-template-columns: minmax(0, 1fr); }
.lib:not(.home) .sg-gob { height: 120px; }
.lib:not(.home) .sg-gob img { left: 22px; height: 120px; }
.lib:not(.home) .sg-body { padding: 14px 22px 0; }
.lib:not(.home) .sg-name { font-size: 22px; }
.lib:not(.home) .sg-rib { margin: 12px -22px 0; }
.made { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
.made li { display: grid; grid-template-columns: 44px minmax(0, 1fr); gap: 10px; padding: 9px 0; border-top: 1px solid var(--line); align-items: center; }
.made li:first-child { border-top: 0; padding-top: 0; }
.date { font-family: var(--f-display); text-align: center; line-height: 1.05; color: var(--wattle-ink); }
.date b { display: block; font-size: 19px; }
.date span { font-size: 10.5px; text-transform: uppercase; letter-spacing: .08em; }
.made a { display: flex; align-items: center; min-height: var(--tap); font-size: 16px; font-weight: 600; color: var(--ink); text-decoration: none; }
.made a:hover { text-decoration: underline; }
.made em { display: block; font-style: normal; font-size: 14px; color: var(--ink-3); }
.forgot { display: grid; grid-template-columns: auto minmax(0, 1fr); column-gap: 14px; align-items: end; }
.forgot h2 { grid-column: 1 / -1; }
.forgot img { display: block; height: 72px; width: auto; transform: scaleX(-1); }
.forgot p { margin: 0; font-size: 15px; color: var(--ink-2); }
.forgot a { display: flex; align-items: center; min-height: var(--tap); font-weight: 600; }
.empty .btn { margin-top: 6px; }
`
