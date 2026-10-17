// The ONE owner of the class string every bounded vertical scroll region in a
// modal, drawer, dropdown, picker or menu uses (T-469).
//
// Why a stable gutter: a region that is `overflow-y-auto` grows a scrollbar
// only while its content overflows. On a platform with classic (space-taking)
// scrollbars, Windows in particular, the scrollbar appearing narrows the
// content box by its width, the content reflows (a detail line that wraps
// differently, a truncation that moves), the new height can drop back under
// the limit, the scrollbar disappears again, and the layout jitters. The New
// Terminal tool picker hit exactly that: its active row expands its detail
// line, so moving between tools flipped the list across the limit.
//
// `scrollbar-gutter: stable` reserves the gutter whether or not the bar is
// showing, so the content width never changes. Overlay-scrollbar platforms
// (macOS defaults, most phones) reserve nothing, so it costs them nothing.
//
// Use the constant instead of spelling `overflow-y-auto` on such a region, so
// the next picker cannot reintroduce the jitter by forgetting the gutter. The
// arbitrary-property spelling is a plain Tailwind class; every app's Tailwind
// `content` includes ../shared/**, so it is never purged.
export const STABLE_SCROLL_Y = "overflow-y-auto [scrollbar-gutter:stable]";
