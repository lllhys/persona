package proto

// UserAllowed and UserDenied read the resolution of a permission card only the user may answer (a persona_settings
// change): the user's own allow or deny. Neither holds for a card that expired, was dismissed, withdrawn or cancelled,
// or that someone else answered for the user (AnsweredBy: the assistant, a supervising conversation). The tool waiting
// on it then says nobody answered, never that the user declined (#3722).
func (r Resolution) UserAllowed() bool { return r.answeredByUser() && *r.Allow }

// UserDenied: see UserAllowed.
func (r Resolution) UserDenied() bool { return r.answeredByUser() && !*r.Allow }

func (r Resolution) answeredByUser() bool {
	return r.Allow != nil && r.AnsweredBy == "" && !r.Dismiss && !r.Cancel
}
