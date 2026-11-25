package publicsummary

type Admission struct {
	shared chan struct{}
	sample chan struct{}
}

func NewAdmission() *Admission {
	return &Admission{shared: make(chan struct{}, 8), sample: make(chan struct{}, 2)}
}

func (a *Admission) TrySummary() bool {
	select {
	case a.shared <- struct{}{}:
		return true
	default:
		return false
	}
}

func (a *Admission) DoneSummary() {
	<-a.shared
}

func (a *Admission) TrySample() bool {
	select {
	case a.sample <- struct{}{}:
	default:
		return false
	}
	if !a.TrySummary() {
		<-a.sample
		return false
	}
	return true
}

func (a *Admission) DoneSample() {
	a.DoneSummary()
	<-a.sample
}
