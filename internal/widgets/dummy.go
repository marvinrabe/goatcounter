package widgets

import (
	"context"
	"html/template"
)

type Dummy struct {
}

func (w Dummy) Name() string                                         { return "dummy" }
func (w Dummy) Type() string                                         { return "hchart" }
func (w Dummy) Label(ctx context.Context) string                     { return "" }
func (w *Dummy) SetHTML(h template.HTML)                             {}
func (w Dummy) HTML() template.HTML                                  { return "" }
func (w *Dummy) SetErr(h error)                                      {}
func (w Dummy) Err() error                                           { return nil }
func (w Dummy) ID() int                                              { return 0 }
func (w *Dummy) SetDetail(d string)                                  {}
func (w Dummy) RenderHTML(context.Context, SharedData) (string, any) { return "", nil }
func (w *Dummy) GetData(ctx context.Context, a Args) (more bool, err error) {
	return false, nil
}
