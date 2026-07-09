package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"toonflow/service"
	"toonflow/service/storyboard"
	"toonflow/task"
)

func (r *Router) storyboardShotUpdateHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	episodeID := c.Param("epId")
	shotNum, err := strconv.Atoi(c.Param("shotNum"))
	if err != nil || shotNum <= 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid shot number"})
		return
	}
	var body struct {
		Speaker  string              `json:"speaker"`
		Text     string              `json:"text"`
		Lines    []task.DialogueLine   `json:"lines"`
		Dialogue json.RawMessage     `json:"dialogue"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	dlg, err := parseDialogueUpdateBody(body.Speaker, body.Text, body.Lines, body.Dialogue)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.UpdateStoryboardShotDialogue(r.db.DB, projectID, episodeID, shotNum, dlg); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "shot_number": shotNum, "dialogue": dlg})
}

func parseDialogueUpdateBody(speaker, text string, lines []task.DialogueLine, raw json.RawMessage) (*task.ShotDialogue, error) {
	if len(lines) > 0 {
		return storyboard.ParseDialogueLines(lines)
	}
	speaker = strings.TrimSpace(speaker)
	text = strings.TrimSpace(text)
	if speaker != "" || text != "" {
		if speaker == "" || text == "" {
			return nil, errDialogueFormat()
		}
		return &task.ShotDialogue{Lines: []task.DialogueLine{{Speaker: speaker, Text: text}}}, nil
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '[' {
		var arr []task.DialogueLine
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, err
		}
		return storyboard.ParseDialogueLines(arr)
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		parsed, err := storyboard.ParseDialogueUserInput(s)
		if err != nil {
			return nil, err
		}
		if parsed.IsEmpty() {
			return nil, nil
		}
		return &parsed, nil
	}
	var dlg task.ShotDialogue
	if err := json.Unmarshal(raw, &dlg); err != nil {
		return nil, err
	}
	if dlg.IsEmpty() {
		return nil, nil
	}
	normalized := dlg.LinesNormalized()
	if len(normalized) == 0 {
		return nil, errDialogueFormat()
	}
	return storyboard.ParseDialogueLines(normalized)
}

func errDialogueFormat() error {
	return &dialogueFormatError{}
}

type dialogueFormatError struct{}

func (e *dialogueFormatError) Error() string {
	return "对白须为 lines 数组，每项包含 speaker 与 text"
}
