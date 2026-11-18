package proto

import "reflect"

// PlainAllowWithEditedText reports whether r is only an Allow with edited_text. Such an answer is valid only on a
// card whose EditableText says its asker can apply the edit; every other answer field would make it a different action.
func (r Resolution) PlainAllowWithEditedText() bool {
	if r.Allow == nil || !*r.Allow || r.EditedText == "" {
		return false
	}
	r.Allow, r.EditedText = nil, ""
	return reflect.ValueOf(r).IsZero()
}
