package cogitator

import "github.com/nerdwave-nick/logis/internal/daemon"

// activation asks the compositor for an xdg-activation token for session
// id's launch of appID and hands it to the daemon (none for a session not
// shown). With the test trace on, a token that came reports `token
// <session>`.
func (u *ui) activation(id daemon.SessionID, appID string) {
	if u.mine(id) == nil {
		u.host.Activated(id, "")
		return
	}
	u.surf.RequestToken(appID, func(token string) {
		if token != "" {
			u.emit("token %d", id)
		}
		u.host.Activated(id, token)
	})
}
