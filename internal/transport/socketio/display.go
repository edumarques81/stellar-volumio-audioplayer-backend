package socketio

import (
	"errors"
	"fmt"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/display"
)

// parseLcdSetMode accepts either a bare mode string or `{mode: "..."}`.
//
// Both shapes are supported deliberately: an earlier handler in this file
// took only the object form, and a client sending the bare string got a
// silent return that looked exactly like a hung backend. Accepting both is
// cheaper than diagnosing that again.
func parseLcdSetMode(args []any) (display.Mode, error) {
	if len(args) == 0 {
		return "", errors.New("lcdSetMode: no payload")
	}

	switch payload := args[0].(type) {
	case string:
		return display.ParseMode(payload)

	case map[string]any:
		raw, ok := payload["mode"]
		if !ok {
			return "", errors.New("lcdSetMode: payload has no 'mode' field")
		}
		name, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("lcdSetMode: 'mode' is %T, want string", raw)
		}
		return display.ParseMode(name)

	default:
		return "", fmt.Errorf("lcdSetMode: payload is %T, want string or object", args[0])
	}
}

// parseLcdSettings applies the fields present in the payload on top of
// current, so a client can send a single changed toggle without having to
// echo the whole schedule back (and without racing another client's edit of
// a field it never touched).
func parseLcdSettings(args []any, current display.Settings) (display.Settings, error) {
	if len(args) == 0 {
		return current, errors.New("setLcdSettings: no payload")
	}
	payload, ok := args[0].(map[string]any)
	if !ok {
		return current, fmt.Errorf("setLcdSettings: payload is %T, want object", args[0])
	}

	next := current
	if v, ok := payload["autoOffEnabled"].(bool); ok {
		next.AutoOffEnabled = v
	}
	if v, ok := payload["autoOffAt"].(string); ok {
		next.AutoOffAt = v
	}
	if v, ok := toInt(payload["autoOffIdleMinutes"]); ok {
		next.AutoOffIdleMinutes = v
	}
	if v, ok := payload["autoRestoreEnabled"].(bool); ok {
		next.AutoRestoreEnabled = v
	}
	if v, ok := payload["autoRestoreAt"].(string); ok {
		next.AutoRestoreAt = v
	}
	return next, nil
}

// toInt accepts the numeric shapes a JSON decoder can hand back.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	default:
		return 0, false
	}
}
