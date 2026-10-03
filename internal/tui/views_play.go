package tui

func (m *model) launchingView() (body, footer string) {
	return m.spin.View() + " Starting " + m.inst.Name + " in Prism Launcher…", ""
}

func (m *model) playingView() (body, footer string) {
	var state string
	switch m.playState {
	case "starting":
		state = m.spin.View() + " Prism Launcher is starting the game…"
	case "slow":
		state = warnSty.Render(wrap("I haven't seen the game start yet. Maybe Prism is asking you something — have a look at its window.", m.bodyWidth()))
	case "running":
		state = okSty.Render("The game is running (since "+m.runningSince.Format("15:04")+").") + "\n" +
			dimSty.Render("Leave me open or press q — the game keeps running either way.")
	case "closed":
		state = "The game closed. Have fun next time!"
	case "unknown":
		state = dimSty.Render(wrap("The game should be starting from Prism now. I can't tell on this computer whether it's running.", m.bodyWidth()))
	}
	b := m.blocks()
	b.title(m.inst.Name)
	b.raw(state)
	return b.String(), m.buttonFooter("enter", "back", "q", "quit")
}
