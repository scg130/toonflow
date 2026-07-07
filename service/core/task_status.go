package core

import (
	"fmt"

	"toonflow/task"
)

// MarkTaskFailed records a user-facing error on the task and returns the message for broadcast.
func MarkTaskFailed(t *task.Task, err error) string {
	if err == nil {
		err = fmt.Errorf("unknown error")
	}
	msg := UserMessageWithLogID(err, t.ID)
	t.SetError(msg)
	return msg
}
