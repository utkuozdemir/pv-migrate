package strategy

type LineTail = lineTail

func NewLineTail(limit int) *LineTail {
	return &lineTail{limit: limit}
}

var (
	FinishRsyncSession         = finishRsyncSession
	PeekAfter                  = peekAfter
	SessionProgressLogger      = sessionProgressLogger
	WriteMidInstallDiagnostics = writeMidInstallDiagnostics
)
