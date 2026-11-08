package substrate

// The wire of an applet's runtime: what a renderer sends to render a record of
// substrate.reamde.dev/applets/applet and to run one of its actions, and the frame
// the server answers. The applet's screens are A2UI v1.0 components
// (https://a2ui.org), and the frame carries A2UI agent-to-renderer messages:
// the substrate is the "agent" side, and a deterministic one, so a tap is a
// query or a write, never a model turn.
//
// Everything a frame carries beside the messages is the host's, not A2UI's:
// the screen to show, the kinds whose changes re-render it and the UI paths
// whose edits do, a notice, and the question an action waits on.

// A2UIVersion is the protocol version every message the runtime writes
// carries, and A2UIBasicCatalog the catalog its components are drawn from.
const (
	A2UIVersion      = "1.0"
	A2UIBasicCatalog = "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"
)

// AppletState is what a renderer holds between frames: the screen it shows, the
// screen's params (the record a detail screen is about), and the UI state
// its inputs write, the data model's /ui. The server keeps none of it.
type AppletState struct {
	Screen string         `json:"screen"`
	Params map[string]any `json:"params,omitempty"`
	UI     map[string]any `json:"ui,omitempty"`
}

// AppletShown is what a renderer is drawing now. A frame for the same screen of
// the same applet version updates the data model alone, so an input keeps its
// focus and its unsent text; anything else replaces the surface. Params say
// which record the screen is about: a screen whose params changed is entered
// again, so its init reads the record they now name.
type AppletShown struct {
	Screen  string         `json:"screen"`
	Version int64          `json:"version"`
	Params  map[string]any `json:"params,omitempty"`
}

// AppletRenderInput renders the applet's current screen. A nil State is the applet's
// start: its first screen and its declared initial UI state.
type AppletRenderInput struct {
	State *AppletState `json:"state,omitempty"`
	Shown *AppletShown `json:"shown,omitempty"`
}

// AppletAction is an A2UI `action` event as the renderer resolved it: the
// action's name and its context, each value already read from the data
// model at the tap.
type AppletAction struct {
	Name              string         `json:"name"`
	Context           map[string]any `json:"context,omitempty"`
	SourceComponentID string         `json:"sourceComponentId,omitempty"`
	// UserMessage is the event's resolved userMessage: what the owner did,
	// in words, as A2UI carries it outbound.
	UserMessage string `json:"userMessage,omitempty"`
}

// AppletActInput runs one of the applet's actions against the state the renderer
// holds. Confirmed answers the question a step's `confirm` asked: until it
// is true, an action with such a step runs nothing.
type AppletActInput struct {
	State     AppletState  `json:"state"`
	Shown     *AppletShown `json:"shown,omitempty"`
	Action    AppletAction `json:"action"`
	Confirmed bool         `json:"confirmed,omitempty"`
}

// AppletFrame is one answer of the runtime: the A2UI messages that bring the
// renderer to the state, and what the host does around them.
type AppletFrame struct {
	Applet AppletHeader `json:"applet"`
	State  AppletState  `json:"state"`
	Shown  AppletShown  `json:"shown"`
	// Messages are A2UI v1.0 agent-to-renderer messages, applied in order.
	Messages []A2UIMessage `json:"messages"`
	// Notice is a line for the host to show once, from an action's
	// `notify` step.
	Notice string `json:"notice,omitempty"`
	// Confirm is the question an action waits on. Nothing ran; the
	// renderer asks it and sends the same action with Confirmed.
	Confirm string `json:"confirm,omitempty"`
	// ConfirmLabel is the answer that runs the action ("Delete", "Save"),
	// and ConfirmDanger says the action deletes, so the renderer draws the
	// answer as a destructive one.
	ConfirmLabel  string `json:"confirmLabel,omitempty"`
	ConfirmDanger bool   `json:"confirmDanger,omitempty"`
}

// AppletHeader names the applet and says what keeps the screen current: the kinds
// its sources read, whose changes re-render it, and the /ui paths whose
// edits re-render it (a filter, a search box).
type AppletHeader struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Version     int64  `json:"version"`
	Title       string `json:"title"`
	// Start is the screen the applet opens on, the one a renderer offers as
	// the way home when another fails.
	Start     string   `json:"start"`
	Watch     []string `json:"watch,omitempty"`
	RefreshOn []string `json:"refreshOn,omitempty"`
	// Destructive names the actions that delete a record, so a renderer
	// draws the buttons that run them as destructive ones.
	Destructive []string `json:"destructive,omitempty"`
}

// A2UIMessage is one A2UI v1.0 agent-to-renderer message: exactly one of
// the four bodies is set.
type A2UIMessage struct {
	Version          string                `json:"version"`
	CreateSurface    *A2UICreateSurface    `json:"createSurface,omitempty"`
	UpdateComponents *A2UIUpdateComponents `json:"updateComponents,omitempty"`
	UpdateDataModel  *A2UIUpdateDataModel  `json:"updateDataModel,omitempty"`
	DeleteSurface    *A2UIDeleteSurface    `json:"deleteSurface,omitempty"`
}

// A2UICreateSurface starts a surface with its components and data model.
// Each component is an A2UI component object as the applet declares it.
type A2UICreateSurface struct {
	SurfaceID  string           `json:"surfaceId"`
	CatalogID  string           `json:"catalogId"`
	Components []map[string]any `json:"components"`
	DataModel  map[string]any   `json:"dataModel"`
}

// A2UIUpdateComponents replaces components of a live surface by id.
type A2UIUpdateComponents struct {
	SurfaceID  string           `json:"surfaceId"`
	Components []map[string]any `json:"components"`
}

// A2UIUpdateDataModel sets the value at a JSON Pointer path; a null value
// removes the key, which is why Value is always written.
type A2UIUpdateDataModel struct {
	SurfaceID string `json:"surfaceId"`
	Path      string `json:"path"`
	Value     any    `json:"value"`
}

// A2UIDeleteSurface removes a surface.
type A2UIDeleteSurface struct {
	SurfaceID string `json:"surfaceId"`
}

// AppletPreviewInput previews one screen of a stored applet (Applet absent)
// or of a draft that was never written (Applet holds its properties).
type AppletPreviewInput struct {
	Applet map[string]any `json:"applet,omitempty"`
	Screen string         `json:"screen,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	UI     map[string]any `json:"ui,omitempty"`
}

// AppletPreview is what an author sees of one screen: the problems a write
// of the applet is refused with, the component tree with its bindings
// resolved against the records the screen read, and each binding that read
// nothing or drew a list as JSON. It writes nothing and runs no action.
type AppletPreview struct {
	Screen     string            `json:"screen"`
	Screens    []string          `json:"screens"`
	Problems   []string          `json:"problems,omitempty"`
	Outline    string            `json:"outline,omitempty"`
	Unresolved []string          `json:"unresolved,omitempty"`
	Counts     map[string]any    `json:"counts,omitempty"`
	Actions    map[string]string `json:"actions,omitempty"`
}
