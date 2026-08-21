package socketio

import (
	"errors"
	"fmt"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/lcd"
)

// parseLcdSetView reads an `lcdSetView` payload.
//
// Two shapes are accepted, because two very different callers emit this:
//   - a bare string ("vu-meter") — the kiosk reporting its own navigation,
//     where there is nothing else to say;
//   - an object {view, wake} — a remote driving the panel, which may also
//     want it woken from standby so the change is actually visible.
//
// `wake` defaults to false: waking the panel is a side effect the caller has
// to ask for, never something inferred from a view change.
func parseLcdSetView(args []any) (lcd.View, bool, error) {
	if len(args) == 0 {
		return "", false, errors.New("lcdSetView: no payload")
	}

	switch payload := args[0].(type) {
	case string:
		view, err := lcd.ParseView(payload)
		return view, false, err

	case map[string]any:
		raw, ok := payload["view"]
		if !ok {
			return "", false, errors.New("lcdSetView: payload has no 'view' field")
		}
		name, ok := raw.(string)
		if !ok {
			return "", false, fmt.Errorf("lcdSetView: 'view' is %T, want string", raw)
		}
		view, err := lcd.ParseView(name)
		if err != nil {
			return "", false, err
		}
		wake, _ := payload["wake"].(bool)
		return view, wake, nil

	default:
		return "", false, fmt.Errorf("lcdSetView: payload is %T, want string or object", args[0])
	}
}
