package ghostty

/*
#cgo CFLAGS: -I${SRCDIR}/../vendor/ghostty/zig-out/include
#cgo LDFLAGS: -L${SRCDIR}/../vendor/ghostty/zig-out/lib -lghostty-vt

#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>
#include <ghostty/vt.h>

// ---------------------------------------------------------------------------
// EffectsContext — passed as terminal userdata to all effect callbacks
// ---------------------------------------------------------------------------
typedef struct {
    int pty_fd;
    int cell_width, cell_height;
    uint16_t cols, rows;
    char title[256];
    int title_changed;
} EffectsContext;

// Forward declare pty_write used by effects
static void ctx_pty_write(int pty_fd, const char *buf, size_t len) {
    while (len > 0) {
        ssize_t n = write(pty_fd, buf, len);
        if (n > 0) { buf += n; len -= (size_t)n; }
        else if (n < 0) {
            if (errno == EINTR) continue;
            break;
        }
    }
}

// ---------------------------------------------------------------------------
// Effect callbacks (same signatures as Ghostling)
// ---------------------------------------------------------------------------
static void effect_write_pty(GhosttyTerminal terminal, void *userdata,
                             const uint8_t *data, size_t len) {
    (void)terminal;
    EffectsContext *ctx = (EffectsContext *)userdata;
    ctx_pty_write(ctx->pty_fd, (const char *)data, len);
}

static bool effect_size(GhosttyTerminal terminal, void *userdata,
                        GhosttySizeReportSize *out_size) {
    (void)terminal;
    EffectsContext *ctx = (EffectsContext *)userdata;
    out_size->rows = ctx->rows;
    out_size->columns = ctx->cols;
    out_size->cell_width = (uint32_t)ctx->cell_width;
    out_size->cell_height = (uint32_t)ctx->cell_height;
    return true;
}

static bool effect_device_attributes(GhosttyTerminal terminal, void *userdata,
                                     GhosttyDeviceAttributes *out_attrs) {
    (void)terminal;
    (void)userdata;
    out_attrs->primary.conformance_level = GHOSTTY_DA_CONFORMANCE_VT220;
    out_attrs->primary.features[0] = GHOSTTY_DA_FEATURE_COLUMNS_132;
    out_attrs->primary.features[1] = GHOSTTY_DA_FEATURE_SELECTIVE_ERASE;
    out_attrs->primary.features[2] = GHOSTTY_DA_FEATURE_ANSI_COLOR;
    out_attrs->primary.num_features = 3;
    out_attrs->secondary.device_type = GHOSTTY_DA_DEVICE_TYPE_VT220;
    out_attrs->secondary.firmware_version = 1;
    out_attrs->secondary.rom_cartridge = 0;
    out_attrs->tertiary.unit_id = 0;
    return true;
}

static GhosttyString effect_xtversion(GhosttyTerminal terminal, void *userdata) {
    (void)terminal;
    (void)userdata;
    return (GhosttyString){ .ptr = (const uint8_t *)"claudehouse", .len = 11 };
}

static void effect_title_changed(GhosttyTerminal terminal, void *userdata) {
    EffectsContext *ctx = (EffectsContext *)userdata;
    GhosttyString title = {0};
    if (ghostty_terminal_get(terminal, GHOSTTY_TERMINAL_DATA_TITLE, &title) != GHOSTTY_SUCCESS)
        return;
    size_t len = title.len < sizeof(ctx->title) - 1 ? title.len : sizeof(ctx->title) - 1;
    memcpy(ctx->title, title.ptr, len);
    ctx->title[len] = '\0';
    ctx->title_changed = 1;
}

static bool effect_color_scheme(GhosttyTerminal terminal, void *userdata,
                                GhosttyColorScheme *out_scheme) {
    (void)terminal;
    (void)userdata;
    (void)out_scheme;
    return false;
}

// ---------------------------------------------------------------------------
// setup_effects — registers all callbacks on a terminal
// ---------------------------------------------------------------------------
static void setup_effects(GhosttyTerminal terminal, EffectsContext *ctx) {
    ghostty_terminal_set(terminal, GHOSTTY_TERMINAL_OPT_USERDATA, ctx);
    ghostty_terminal_set(terminal, GHOSTTY_TERMINAL_OPT_WRITE_PTY,
        (const void *)effect_write_pty);
    ghostty_terminal_set(terminal, GHOSTTY_TERMINAL_OPT_SIZE,
        (const void *)effect_size);
    ghostty_terminal_set(terminal, GHOSTTY_TERMINAL_OPT_DEVICE_ATTRIBUTES,
        (const void *)effect_device_attributes);
    ghostty_terminal_set(terminal, GHOSTTY_TERMINAL_OPT_XTVERSION,
        (const void *)effect_xtversion);
    ghostty_terminal_set(terminal, GHOSTTY_TERMINAL_OPT_TITLE_CHANGED,
        (const void *)effect_title_changed);
    ghostty_terminal_set(terminal, GHOSTTY_TERMINAL_OPT_COLOR_SCHEME,
        (const void *)effect_color_scheme);
}

// ---------------------------------------------------------------------------
// create_* helpers — avoid CGo NULL typing issues with _new() functions
// ---------------------------------------------------------------------------
static GhosttyResult create_terminal(GhosttyTerminal *out, uint16_t cols, uint16_t rows, uint32_t max_scrollback) {
    GhosttyTerminalOptions opts = { .cols = cols, .rows = rows, .max_scrollback = max_scrollback };
    return ghostty_terminal_new(NULL, out, opts);
}

static GhosttyResult create_key_encoder(GhosttyKeyEncoder *out) {
    return ghostty_key_encoder_new(NULL, out);
}

static GhosttyResult create_key_event(GhosttyKeyEvent *out) {
    return ghostty_key_event_new(NULL, out);
}

static GhosttyResult create_mouse_encoder(GhosttyMouseEncoder *out) {
    return ghostty_mouse_encoder_new(NULL, out);
}

static GhosttyResult create_mouse_event(GhosttyMouseEvent *out) {
    return ghostty_mouse_event_new(NULL, out);
}

static GhosttyResult create_render_state(GhosttyRenderState *out) {
    return ghostty_render_state_new(NULL, out);
}

static GhosttyResult create_row_iterator(GhosttyRenderStateRowIterator *out) {
    return ghostty_render_state_row_iterator_new(NULL, out);
}

static GhosttyResult create_row_cells(GhosttyRenderStateRowCells *out) {
    return ghostty_render_state_row_cells_new(NULL, out);
}

// ---------------------------------------------------------------------------
// init_sized_* helpers — expand GHOSTTY_INIT_SIZED() macro
// ---------------------------------------------------------------------------
static GhosttyRenderStateColors init_render_state_colors() {
    GhosttyRenderStateColors c = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
    return c;
}

static GhosttyStyle init_style() {
    GhosttyStyle s = GHOSTTY_INIT_SIZED(GhosttyStyle);
    return s;
}

static GhosttyMouseEncoderSize init_mouse_encoder_size() {
    GhosttyMouseEncoderSize s = GHOSTTY_INIT_SIZED(GhosttyMouseEncoderSize);
    return s;
}

// ---------------------------------------------------------------------------
// Key/mouse encode helpers
// ---------------------------------------------------------------------------
static GhosttyResult encode_key(GhosttyKeyEncoder enc, GhosttyKeyEvent evt,
                                 char *buf, size_t buflen, size_t *written) {
    return ghostty_key_encoder_encode(enc, evt, buf, buflen, written);
}

static GhosttyResult encode_mouse(GhosttyMouseEncoder enc, GhosttyMouseEvent evt,
                                   char *buf, size_t buflen, size_t *written) {
    return ghostty_mouse_encoder_encode(enc, evt, buf, buflen, written);
}

static GhosttyResult encode_focus(GhosttyFocusEvent evt, char *buf, size_t buflen, size_t *written) {
    return ghostty_focus_encode(evt, buf, buflen, written);
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Value types
// ---------------------------------------------------------------------------

type Key = C.GhosttyKey
type Mods = C.GhosttyMods
type KeyAction = C.GhosttyKeyAction
type MouseButton = C.GhosttyMouseButton
type MouseAction = C.GhosttyMouseAction

type ColorRGB struct {
	R, G, B uint8
}

type Style struct {
	Bold, Italic, Inverse bool
}

type RenderColors struct {
	Foreground, Background, Cursor ColorRGB
	CursorHasValue                 bool
}

// ---------------------------------------------------------------------------
// Key constants — initialized from C values
// ---------------------------------------------------------------------------

var (
	KeyA              Key
	KeyB              Key
	KeyC              Key
	KeyD              Key
	KeyE              Key
	KeyF              Key
	KeyG              Key
	KeyH              Key
	KeyI              Key
	KeyJ              Key
	KeyK              Key
	KeyL              Key
	KeyM              Key
	KeyN              Key
	KeyO              Key
	KeyP              Key
	KeyQ              Key
	KeyR              Key
	KeyS              Key
	KeyT              Key
	KeyU              Key
	KeyV              Key
	KeyW              Key
	KeyX              Key
	KeyY              Key
	KeyZ              Key
	KeyDigit0         Key
	KeyDigit1         Key
	KeyDigit2         Key
	KeyDigit3         Key
	KeyDigit4         Key
	KeyDigit5         Key
	KeyDigit6         Key
	KeyDigit7         Key
	KeyDigit8         Key
	KeyDigit9         Key
	KeyF1             Key
	KeyF2             Key
	KeyF3             Key
	KeyF4             Key
	KeyF5             Key
	KeyF6             Key
	KeyF7             Key
	KeyF8             Key
	KeyF9             Key
	KeyF10            Key
	KeyF11            Key
	KeyF12            Key
	KeySpace          Key
	KeyEnter          Key
	KeyTab            Key
	KeyBackspace      Key
	KeyDelete         Key
	KeyEscape         Key
	KeyArrowUp        Key
	KeyArrowDown      Key
	KeyArrowLeft      Key
	KeyArrowRight     Key
	KeyHome           Key
	KeyEnd            Key
	KeyPageUp         Key
	KeyPageDown       Key
	KeyInsert         Key
	KeyMinus          Key
	KeyEqual          Key
	KeyBracketLeft    Key
	KeyBracketRight   Key
	KeyBackslash      Key
	KeySemicolon      Key
	KeyQuote          Key
	KeyComma          Key
	KeyPeriod         Key
	KeySlash          Key
	KeyBackquote      Key
	KeyUnidentified   Key

	ModsShift Mods
	ModsCtrl  Mods
	ModsAlt   Mods
	ModsSuper Mods

	KeyActionPress   KeyAction
	KeyActionRelease KeyAction
	KeyActionRepeat  KeyAction

	MouseButtonLeft    MouseButton
	MouseButtonRight   MouseButton
	MouseButtonMiddle  MouseButton
	MouseButtonFour    MouseButton
	MouseButtonFive    MouseButton
	MouseButtonSix     MouseButton
	MouseButtonSeven   MouseButton
	MouseButtonUnknown MouseButton

	MouseActionPress   MouseAction
	MouseActionRelease MouseAction
	MouseActionMotion  MouseAction

	FocusGained = C.GHOSTTY_FOCUS_GAINED
	FocusLost   = C.GHOSTTY_FOCUS_LOST
)

func init() {
	KeyA = C.GHOSTTY_KEY_A
	KeyB = C.GHOSTTY_KEY_B
	KeyC = C.GHOSTTY_KEY_C
	KeyD = C.GHOSTTY_KEY_D
	KeyE = C.GHOSTTY_KEY_E
	KeyF = C.GHOSTTY_KEY_F
	KeyG = C.GHOSTTY_KEY_G
	KeyH = C.GHOSTTY_KEY_H
	KeyI = C.GHOSTTY_KEY_I
	KeyJ = C.GHOSTTY_KEY_J
	KeyK = C.GHOSTTY_KEY_K
	KeyL = C.GHOSTTY_KEY_L
	KeyM = C.GHOSTTY_KEY_M
	KeyN = C.GHOSTTY_KEY_N
	KeyO = C.GHOSTTY_KEY_O
	KeyP = C.GHOSTTY_KEY_P
	KeyQ = C.GHOSTTY_KEY_Q
	KeyR = C.GHOSTTY_KEY_R
	KeyS = C.GHOSTTY_KEY_S
	KeyT = C.GHOSTTY_KEY_T
	KeyU = C.GHOSTTY_KEY_U
	KeyV = C.GHOSTTY_KEY_V
	KeyW = C.GHOSTTY_KEY_W
	KeyX = C.GHOSTTY_KEY_X
	KeyY = C.GHOSTTY_KEY_Y
	KeyZ = C.GHOSTTY_KEY_Z
	KeyDigit0 = C.GHOSTTY_KEY_DIGIT_0
	KeyDigit1 = C.GHOSTTY_KEY_DIGIT_1
	KeyDigit2 = C.GHOSTTY_KEY_DIGIT_2
	KeyDigit3 = C.GHOSTTY_KEY_DIGIT_3
	KeyDigit4 = C.GHOSTTY_KEY_DIGIT_4
	KeyDigit5 = C.GHOSTTY_KEY_DIGIT_5
	KeyDigit6 = C.GHOSTTY_KEY_DIGIT_6
	KeyDigit7 = C.GHOSTTY_KEY_DIGIT_7
	KeyDigit8 = C.GHOSTTY_KEY_DIGIT_8
	KeyDigit9 = C.GHOSTTY_KEY_DIGIT_9
	KeyF1 = C.GHOSTTY_KEY_F1
	KeyF2 = C.GHOSTTY_KEY_F2
	KeyF3 = C.GHOSTTY_KEY_F3
	KeyF4 = C.GHOSTTY_KEY_F4
	KeyF5 = C.GHOSTTY_KEY_F5
	KeyF6 = C.GHOSTTY_KEY_F6
	KeyF7 = C.GHOSTTY_KEY_F7
	KeyF8 = C.GHOSTTY_KEY_F8
	KeyF9 = C.GHOSTTY_KEY_F9
	KeyF10 = C.GHOSTTY_KEY_F10
	KeyF11 = C.GHOSTTY_KEY_F11
	KeyF12 = C.GHOSTTY_KEY_F12
	KeySpace = C.GHOSTTY_KEY_SPACE
	KeyEnter = C.GHOSTTY_KEY_ENTER
	KeyTab = C.GHOSTTY_KEY_TAB
	KeyBackspace = C.GHOSTTY_KEY_BACKSPACE
	KeyDelete = C.GHOSTTY_KEY_DELETE
	KeyEscape = C.GHOSTTY_KEY_ESCAPE
	KeyArrowUp = C.GHOSTTY_KEY_ARROW_UP
	KeyArrowDown = C.GHOSTTY_KEY_ARROW_DOWN
	KeyArrowLeft = C.GHOSTTY_KEY_ARROW_LEFT
	KeyArrowRight = C.GHOSTTY_KEY_ARROW_RIGHT
	KeyHome = C.GHOSTTY_KEY_HOME
	KeyEnd = C.GHOSTTY_KEY_END
	KeyPageUp = C.GHOSTTY_KEY_PAGE_UP
	KeyPageDown = C.GHOSTTY_KEY_PAGE_DOWN
	KeyInsert = C.GHOSTTY_KEY_INSERT
	KeyMinus = C.GHOSTTY_KEY_MINUS
	KeyEqual = C.GHOSTTY_KEY_EQUAL
	KeyBracketLeft = C.GHOSTTY_KEY_BRACKET_LEFT
	KeyBracketRight = C.GHOSTTY_KEY_BRACKET_RIGHT
	KeyBackslash = C.GHOSTTY_KEY_BACKSLASH
	KeySemicolon = C.GHOSTTY_KEY_SEMICOLON
	KeyQuote = C.GHOSTTY_KEY_QUOTE
	KeyComma = C.GHOSTTY_KEY_COMMA
	KeyPeriod = C.GHOSTTY_KEY_PERIOD
	KeySlash = C.GHOSTTY_KEY_SLASH
	KeyBackquote = C.GHOSTTY_KEY_BACKQUOTE
	KeyUnidentified = C.GHOSTTY_KEY_UNIDENTIFIED

	ModsShift = C.GHOSTTY_MODS_SHIFT
	ModsCtrl = C.GHOSTTY_MODS_CTRL
	ModsAlt = C.GHOSTTY_MODS_ALT
	ModsSuper = C.GHOSTTY_MODS_SUPER

	KeyActionPress = C.GHOSTTY_KEY_ACTION_PRESS
	KeyActionRelease = C.GHOSTTY_KEY_ACTION_RELEASE
	KeyActionRepeat = C.GHOSTTY_KEY_ACTION_REPEAT

	MouseButtonLeft = C.GHOSTTY_MOUSE_BUTTON_LEFT
	MouseButtonRight = C.GHOSTTY_MOUSE_BUTTON_RIGHT
	MouseButtonMiddle = C.GHOSTTY_MOUSE_BUTTON_MIDDLE
	MouseButtonFour = C.GHOSTTY_MOUSE_BUTTON_FOUR
	MouseButtonFive = C.GHOSTTY_MOUSE_BUTTON_FIVE
	MouseButtonSix = C.GHOSTTY_MOUSE_BUTTON_SIX
	MouseButtonSeven = C.GHOSTTY_MOUSE_BUTTON_SEVEN
	MouseButtonUnknown = C.GHOSTTY_MOUSE_BUTTON_UNKNOWN

	MouseActionPress = C.GHOSTTY_MOUSE_ACTION_PRESS
	MouseActionRelease = C.GHOSTTY_MOUSE_ACTION_RELEASE
	MouseActionMotion = C.GHOSTTY_MOUSE_ACTION_MOTION
}

// ---------------------------------------------------------------------------
// Terminal
// ---------------------------------------------------------------------------

type Terminal struct {
	term       C.GhosttyTerminal
	effectsCtx *C.EffectsContext
}

func NewTerminal(cols, rows uint16, maxScrollback uint32) (*Terminal, error) {
	t := &Terminal{}
	res := C.create_terminal(&t.term, C.uint16_t(cols), C.uint16_t(rows), C.uint32_t(maxScrollback))
	if res != C.GHOSTTY_SUCCESS {
		return nil, errors.New("ghostty_terminal_new failed")
	}
	t.effectsCtx = (*C.EffectsContext)(C.calloc(1, C.size_t(unsafe.Sizeof(C.EffectsContext{}))))
	return t, nil
}

func (t *Terminal) Free() {
	if t.term != nil {
		C.ghostty_terminal_free(t.term)
		t.term = nil
	}
	if t.effectsCtx != nil {
		C.free(unsafe.Pointer(t.effectsCtx))
		t.effectsCtx = nil
	}
}

func (t *Terminal) VTWrite(data []byte) {
	if len(data) == 0 {
		return
	}
	C.ghostty_terminal_vt_write(t.term, (*C.uint8_t)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
}

func (t *Terminal) Resize(cols, rows uint16, cellW, cellH int) {
	C.ghostty_terminal_resize(t.term, C.uint16_t(cols), C.uint16_t(rows),
		C.uint32_t(cellW), C.uint32_t(cellH))
}

func (t *Terminal) SetEffects(ptyFd, cellW, cellH int, cols, rows uint16) {
	t.effectsCtx.pty_fd = C.int(ptyFd)
	t.effectsCtx.cell_width = C.int(cellW)
	t.effectsCtx.cell_height = C.int(cellH)
	t.effectsCtx.cols = C.uint16_t(cols)
	t.effectsCtx.rows = C.uint16_t(rows)
	C.setup_effects(t.term, t.effectsCtx)
}

func (t *Terminal) UpdateEffectsSize(cols, rows uint16) {
	t.effectsCtx.cols = C.uint16_t(cols)
	t.effectsCtx.rows = C.uint16_t(rows)
}

func (t *Terminal) ScrollViewport(delta int) {
	sv := C.GhosttyTerminalScrollViewport{
		tag: C.GHOSTTY_SCROLL_VIEWPORT_DELTA,
	}
	// Set the delta value in the union
	*(*C.intptr_t)(unsafe.Pointer(&sv.value)) = C.intptr_t(delta)
	C.ghostty_terminal_scroll_viewport(t.term, sv)
}

func (t *Terminal) ModeGet(mode C.GhosttyMode) bool {
	var val C.bool
	C.ghostty_terminal_mode_get(t.term, mode, &val)
	return bool(val)
}

func (t *Terminal) GetTitle() (string, bool) {
	if t.effectsCtx.title_changed != 0 {
		t.effectsCtx.title_changed = 0
		return C.GoString(&t.effectsCtx.title[0]), true
	}
	return "", false
}

func (t *Terminal) GetMouseTracking() bool {
	var tracking C.bool
	C.ghostty_terminal_get(t.term, C.GHOSTTY_TERMINAL_DATA_MOUSE_TRACKING, unsafe.Pointer(&tracking))
	return bool(tracking)
}

func (t *Terminal) Handle() C.GhosttyTerminal {
	return t.term
}

// ModeFocusEvent returns the GHOSTTY_MODE_FOCUS_EVENT constant.
func ModeFocusEvent() C.GhosttyMode {
	return C.GHOSTTY_MODE_FOCUS_EVENT
}

// ---------------------------------------------------------------------------
// RenderState
// ---------------------------------------------------------------------------

type RenderState struct {
	state C.GhosttyRenderState
}

func NewRenderState() (*RenderState, error) {
	rs := &RenderState{}
	res := C.create_render_state(&rs.state)
	if res != C.GHOSTTY_SUCCESS {
		return nil, errors.New("ghostty_render_state_new failed")
	}
	return rs, nil
}

func (rs *RenderState) Free() {
	if rs.state != nil {
		C.ghostty_render_state_free(rs.state)
		rs.state = nil
	}
}

func (rs *RenderState) Update(t *Terminal) {
	C.ghostty_render_state_update(rs.state, t.term)
}

func (rs *RenderState) GetColors() RenderColors {
	colors := C.init_render_state_colors()
	C.ghostty_render_state_colors_get(rs.state, &colors)
	return RenderColors{
		Foreground:     ColorRGB{uint8(colors.foreground.r), uint8(colors.foreground.g), uint8(colors.foreground.b)},
		Background:     ColorRGB{uint8(colors.background.r), uint8(colors.background.g), uint8(colors.background.b)},
		Cursor:         ColorRGB{uint8(colors.cursor.r), uint8(colors.cursor.g), uint8(colors.cursor.b)},
		CursorHasValue: bool(colors.cursor_has_value),
	}
}

func (rs *RenderState) GetCursorVisible() bool {
	var visible C.bool
	C.ghostty_render_state_get(rs.state, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE, unsafe.Pointer(&visible))
	return bool(visible)
}

func (rs *RenderState) GetCursorInViewport() bool {
	var inViewport C.bool
	C.ghostty_render_state_get(rs.state, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE, unsafe.Pointer(&inViewport))
	return bool(inViewport)
}

func (rs *RenderState) GetCursorPos() (x, y uint16) {
	var cx, cy C.uint16_t
	C.ghostty_render_state_get(rs.state, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X, unsafe.Pointer(&cx))
	C.ghostty_render_state_get(rs.state, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y, unsafe.Pointer(&cy))
	return uint16(cx), uint16(cy)
}

func (rs *RenderState) SetClean() {
	clean := C.GhosttyRenderStateDirty(C.GHOSTTY_RENDER_STATE_DIRTY_FALSE)
	C.ghostty_render_state_set(rs.state, C.GHOSTTY_RENDER_STATE_OPTION_DIRTY, unsafe.Pointer(&clean))
}

func (rs *RenderState) Handle() C.GhosttyRenderState {
	return rs.state
}

// ---------------------------------------------------------------------------
// RowIterator
// ---------------------------------------------------------------------------

type RowIterator struct {
	iter C.GhosttyRenderStateRowIterator
}

func NewRowIterator() (*RowIterator, error) {
	ri := &RowIterator{}
	res := C.create_row_iterator(&ri.iter)
	if res != C.GHOSTTY_SUCCESS {
		return nil, errors.New("ghostty_render_state_row_iterator_new failed")
	}
	return ri, nil
}

func (ri *RowIterator) Free() {
	if ri.iter != nil {
		C.ghostty_render_state_row_iterator_free(ri.iter)
		ri.iter = nil
	}
}

func (ri *RowIterator) Init(rs *RenderState) bool {
	res := C.ghostty_render_state_get(rs.state,
		C.GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, unsafe.Pointer(&ri.iter))
	return res == C.GHOSTTY_SUCCESS
}

func (ri *RowIterator) Next() bool {
	return bool(C.ghostty_render_state_row_iterator_next(ri.iter))
}

func (ri *RowIterator) GetCells(rc *RowCells) bool {
	res := C.ghostty_render_state_row_get(ri.iter,
		C.GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, unsafe.Pointer(&rc.cells))
	return res == C.GHOSTTY_SUCCESS
}

func (ri *RowIterator) SetClean() {
	clean := C.bool(false)
	C.ghostty_render_state_row_set(ri.iter,
		C.GHOSTTY_RENDER_STATE_ROW_OPTION_DIRTY, unsafe.Pointer(&clean))
}

// ---------------------------------------------------------------------------
// RowCells
// ---------------------------------------------------------------------------

type RowCells struct {
	cells C.GhosttyRenderStateRowCells
}

func NewRowCells() (*RowCells, error) {
	rc := &RowCells{}
	res := C.create_row_cells(&rc.cells)
	if res != C.GHOSTTY_SUCCESS {
		return nil, errors.New("ghostty_render_state_row_cells_new failed")
	}
	return rc, nil
}

func (rc *RowCells) Free() {
	if rc.cells != nil {
		C.ghostty_render_state_row_cells_free(rc.cells)
		rc.cells = nil
	}
}

func (rc *RowCells) Next() bool {
	return bool(C.ghostty_render_state_row_cells_next(rc.cells))
}

func (rc *RowCells) GraphemeLen() uint32 {
	var length C.uint32_t
	C.ghostty_render_state_row_cells_get(rc.cells,
		C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN, unsafe.Pointer(&length))
	return uint32(length)
}

func (rc *RowCells) Graphemes(buf []uint32) {
	if len(buf) == 0 {
		return
	}
	C.ghostty_render_state_row_cells_get(rc.cells,
		C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF, unsafe.Pointer(&buf[0]))
}

func (rc *RowCells) FgColor() (ColorRGB, bool) {
	var fg C.GhosttyColorRgb
	res := C.ghostty_render_state_row_cells_get(rc.cells,
		C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, unsafe.Pointer(&fg))
	if res == C.GHOSTTY_SUCCESS {
		return ColorRGB{uint8(fg.r), uint8(fg.g), uint8(fg.b)}, true
	}
	return ColorRGB{}, false
}

func (rc *RowCells) BgColor() (ColorRGB, bool) {
	var bg C.GhosttyColorRgb
	res := C.ghostty_render_state_row_cells_get(rc.cells,
		C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, unsafe.Pointer(&bg))
	if res == C.GHOSTTY_SUCCESS {
		return ColorRGB{uint8(bg.r), uint8(bg.g), uint8(bg.b)}, true
	}
	return ColorRGB{}, false
}

func (rc *RowCells) GetStyle() Style {
	style := C.init_style()
	C.ghostty_render_state_row_cells_get(rc.cells,
		C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, unsafe.Pointer(&style))
	return Style{
		Bold:    bool(style.bold),
		Italic:  bool(style.italic),
		Inverse: bool(style.inverse),
	}
}

// ---------------------------------------------------------------------------
// KeyEncoder
// ---------------------------------------------------------------------------

type KeyEncoder struct {
	enc C.GhosttyKeyEncoder
}

func NewKeyEncoder() (*KeyEncoder, error) {
	ke := &KeyEncoder{}
	res := C.create_key_encoder(&ke.enc)
	if res != C.GHOSTTY_SUCCESS {
		return nil, errors.New("ghostty_key_encoder_new failed")
	}
	return ke, nil
}

func (ke *KeyEncoder) Free() {
	if ke.enc != nil {
		C.ghostty_key_encoder_free(ke.enc)
		ke.enc = nil
	}
}

func (ke *KeyEncoder) SetOptFromTerminal(t *Terminal) {
	C.ghostty_key_encoder_setopt_from_terminal(ke.enc, t.term)
}

func (ke *KeyEncoder) Encode(evt *KeyEvent, buf []byte) int {
	if len(buf) == 0 {
		return 0
	}
	var written C.size_t
	res := C.encode_key(ke.enc, evt.evt, (*C.char)(unsafe.Pointer(&buf[0])),
		C.size_t(len(buf)), &written)
	if res == C.GHOSTTY_SUCCESS {
		return int(written)
	}
	return 0
}

// ---------------------------------------------------------------------------
// KeyEvent
// ---------------------------------------------------------------------------

type KeyEvent struct {
	evt C.GhosttyKeyEvent
}

func NewKeyEvent() (*KeyEvent, error) {
	ke := &KeyEvent{}
	res := C.create_key_event(&ke.evt)
	if res != C.GHOSTTY_SUCCESS {
		return nil, errors.New("ghostty_key_event_new failed")
	}
	return ke, nil
}

func (ke *KeyEvent) Free() {
	if ke.evt != nil {
		C.ghostty_key_event_free(ke.evt)
		ke.evt = nil
	}
}

func (ke *KeyEvent) SetKey(key Key) {
	C.ghostty_key_event_set_key(ke.evt, key)
}

func (ke *KeyEvent) SetAction(action KeyAction) {
	C.ghostty_key_event_set_action(ke.evt, action)
}

func (ke *KeyEvent) SetMods(mods Mods) {
	C.ghostty_key_event_set_mods(ke.evt, mods)
}

func (ke *KeyEvent) SetUnshiftedCodepoint(cp uint32) {
	C.ghostty_key_event_set_unshifted_codepoint(ke.evt, C.uint32_t(cp))
}

func (ke *KeyEvent) SetConsumedMods(mods Mods) {
	C.ghostty_key_event_set_consumed_mods(ke.evt, mods)
}

func (ke *KeyEvent) SetUtf8(data []byte) {
	if len(data) == 0 {
		C.ghostty_key_event_set_utf8(ke.evt, nil, 0)
		return
	}
	C.ghostty_key_event_set_utf8(ke.evt, (*C.char)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
}

// ---------------------------------------------------------------------------
// MouseEncoder
// ---------------------------------------------------------------------------

type MouseEncoder struct {
	enc C.GhosttyMouseEncoder
}

func NewMouseEncoder() (*MouseEncoder, error) {
	me := &MouseEncoder{}
	res := C.create_mouse_encoder(&me.enc)
	if res != C.GHOSTTY_SUCCESS {
		return nil, errors.New("ghostty_mouse_encoder_new failed")
	}
	return me, nil
}

func (me *MouseEncoder) Free() {
	if me.enc != nil {
		C.ghostty_mouse_encoder_free(me.enc)
		me.enc = nil
	}
}

func (me *MouseEncoder) SetOptFromTerminal(t *Terminal) {
	C.ghostty_mouse_encoder_setopt_from_terminal(me.enc, t.term)
}

func (me *MouseEncoder) SetSize(screenW, screenH, cellW, cellH, pad int) {
	s := C.init_mouse_encoder_size()
	s.screen_width = C.uint32_t(screenW)
	s.screen_height = C.uint32_t(screenH)
	s.cell_width = C.uint32_t(cellW)
	s.cell_height = C.uint32_t(cellH)
	s.padding_top = C.uint32_t(pad)
	s.padding_bottom = C.uint32_t(pad)
	s.padding_left = C.uint32_t(pad)
	s.padding_right = C.uint32_t(pad)
	C.ghostty_mouse_encoder_setopt(me.enc, C.GHOSTTY_MOUSE_ENCODER_OPT_SIZE, unsafe.Pointer(&s))
}

func (me *MouseEncoder) SetAnyButtonPressed(pressed bool) {
	val := C.bool(pressed)
	C.ghostty_mouse_encoder_setopt(me.enc, C.GHOSTTY_MOUSE_ENCODER_OPT_ANY_BUTTON_PRESSED, unsafe.Pointer(&val))
}

func (me *MouseEncoder) SetTrackLastCell(track bool) {
	val := C.bool(track)
	C.ghostty_mouse_encoder_setopt(me.enc, C.GHOSTTY_MOUSE_ENCODER_OPT_TRACK_LAST_CELL, unsafe.Pointer(&val))
}

func (me *MouseEncoder) Encode(evt *MouseEvent, buf []byte) int {
	if len(buf) == 0 {
		return 0
	}
	var written C.size_t
	res := C.encode_mouse(me.enc, evt.evt, (*C.char)(unsafe.Pointer(&buf[0])),
		C.size_t(len(buf)), &written)
	if res == C.GHOSTTY_SUCCESS {
		return int(written)
	}
	return 0
}

// ---------------------------------------------------------------------------
// MouseEvent
// ---------------------------------------------------------------------------

type MouseEvent struct {
	evt C.GhosttyMouseEvent
}

func NewMouseEvent() (*MouseEvent, error) {
	me := &MouseEvent{}
	res := C.create_mouse_event(&me.evt)
	if res != C.GHOSTTY_SUCCESS {
		return nil, errors.New("ghostty_mouse_event_new failed")
	}
	return me, nil
}

func (me *MouseEvent) Free() {
	if me.evt != nil {
		C.ghostty_mouse_event_free(me.evt)
		me.evt = nil
	}
}

func (me *MouseEvent) SetAction(action MouseAction) {
	C.ghostty_mouse_event_set_action(me.evt, action)
}

func (me *MouseEvent) SetButton(btn MouseButton) {
	C.ghostty_mouse_event_set_button(me.evt, btn)
}

func (me *MouseEvent) ClearButton() {
	C.ghostty_mouse_event_clear_button(me.evt)
}

func (me *MouseEvent) SetMods(mods Mods) {
	C.ghostty_mouse_event_set_mods(me.evt, mods)
}

func (me *MouseEvent) SetPosition(x, y float32) {
	pos := C.GhosttyMousePosition{x: C.float(x), y: C.float(y)}
	C.ghostty_mouse_event_set_position(me.evt, pos)
}

// ---------------------------------------------------------------------------
// FocusEncode
// ---------------------------------------------------------------------------

func FocusEncode(gained bool, buf []byte) int {
	if len(buf) == 0 {
		return 0
	}
	var evt C.GhosttyFocusEvent
	if gained {
		evt = C.GHOSTTY_FOCUS_GAINED
	} else {
		evt = C.GHOSTTY_FOCUS_LOST
	}
	var written C.size_t
	res := C.encode_focus(evt, (*C.char)(unsafe.Pointer(&buf[0])),
		C.size_t(len(buf)), &written)
	if res == C.GHOSTTY_SUCCESS {
		return int(written)
	}
	return 0
}
