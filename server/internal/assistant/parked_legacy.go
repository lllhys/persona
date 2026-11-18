package assistant

import "context"

// legacyOwnerPriorityReview is the latest review's old owner-priority park, not a new question or owner consent.
// Keep identifying it after reconciliation or an answer: its old recommendation must not become a default.
func (v *Supervision) legacyOwnerPriorityReview() *TaskReview {
	if v == nil || len(v.Reviews) == 0 {
		return nil
	}
	r := &v.Reviews[len(v.Reviews)-1]
	if r.Verdict == reviewParked && r.WaitingOn == parkOwnerPriority {
		return r
	}
	return nil
}

// decisionPark also recognizes the legacy owner wait moved to needs_user, until a real review replaces it.
func (v *Supervision) decisionPark() *TaskReview {
	if r := v.parking(); r != nil {
		return r
	}
	if v != nil && v.State == reviewNeedsUser {
		if r := v.legacyOwnerPriorityReview(); r != nil && r.Decided == "" {
			return r
		}
	}
	return nil
}

// reconcileLegacyOwnerPriority makes an old owner choice visible without reconstructing a question. Missing
// details go through undecidableSignal in this watch; actual owner evidence, including a failed read/write, wins.
func (s *Service) reconcileLegacyOwnerPriority(ctx context.Context, t Todo, ownerEvidencePending bool) Todo {
	r := t.Supervision.legacyOwnerPriorityReview()
	if t.DoneAt != nil || r == nil {
		return t
	}
	if t.Supervision.State == reviewParked && r.Decided == "" && !ownerEvidencePending {
		_, _, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
			p := v.parking()
			if p == nil || p.WaitingOn != parkOwnerPriority || !p.At.Equal(r.At) || p.Decided != "" {
				return false
			}
			if current, ok := s.todoNow(t.ID); !ok || current.DoneAt != nil {
				return false
			}
			v.State = reviewNeedsUser
			return true
		})
		if err != nil {
			s.logf("task %s: reconcile legacy owner-priority wait: %v (retried by the watch)", t.ID, err)
		}
	}
	if current, ok := s.todoNow(t.ID); ok {
		return current
	}
	return t
}
