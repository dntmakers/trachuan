package releasechange

import "io"

type ChangeInputs struct {
	Current              string
	Predecessor          string
	Members              Members
	Bindings             ChangesBindings
	CheckCurrentBindings bool
}

func (in ChangeInputs) internal() changeInputs {
	return changeInputs{current: in.Current, predecessor: in.Predecessor, Members: in.Members, bindings: in.Bindings, checkCurrentBindings: in.CheckCurrentBindings}
}

func Generate(in ChangeInputs, emit func(any) error) error { return generate(in.internal(), emit) }

func WriteChanges(w io.Writer, in ChangeInputs) error { return writeChanges(w, in.internal()) }
