// Package finding holds the findings model and the pure decision rules
// (judges, Group B filter, verdict). It performs no I/O.
package finding

// Severity is the severity of a finding.
type Severity string

const (
	SeverityCritico Severity = "critico"
	SeverityAlto    Severity = "alto"
	SeverityMedio   Severity = "medio"
	SeverityBajo    Severity = "bajo"
)

// Grave reports whether the severity is critico or alto.
func (s Severity) Grave() bool { return s == SeverityCritico || s == SeverityAlto }

// Origin tells whether a finding comes from the PR.
type Origin string

const (
	OriginIntroducido  Origin = "introducido"
	OriginActivado     Origin = "activado"
	OriginEmpeorado    Origin = "empeorado"
	OriginPreexistente Origin = "preexistente"
	OriginIncierto     Origin = "incierto"
)

// FromPR reports whether the finding was introduced, activated or worsened by the PR.
func (o Origin) FromPR() bool {
	return o == OriginIntroducido || o == OriginActivado || o == OriginEmpeorado
}

// Group is the review policy group of a finding.
type Group string

const (
	GroupA Group = "A"
	GroupB Group = "B"
)

// Status is the validation state of a finding.
type Status string

const (
	StatusCandidato           Status = "candidato"
	StatusConfirmado          Status = "confirmado"
	StatusRefutado            Status = "refutado"
	StatusSinConfirmar        Status = "sin_confirmar"
	StatusMenor               Status = "menor"
	StatusDescartadoPorJueces Status = "descartado_por_jueces"
	// StatusFiltrado is a Group B finding hidden by the Group B filter: no
	// confirmed Group A finding in its file or function. It is kept for the
	// record and never counts.
	StatusFiltrado Status = "filtrado"
)

// FixStatusARevisar marks a fix the judges flagged as violating a repo rule.
const FixStatusARevisar = "a_revisar"

// RuleRef is an exact quote of a repo rule and the file it lives in.
type RuleRef struct {
	Quote string `json:"cita"`
	File  string `json:"archivo"`
}

// Doubt is a design question raised by an agent.
type Doubt struct {
	File string `json:"archivo"`
	Line int    `json:"linea"`
	Text string `json:"texto"`
}

// Finding is a single review finding.
type Finding struct {
	ID        string      `json:"id"`
	Role      string      `json:"rol"`
	File      string      `json:"archivo"`
	Line      int         `json:"linea"`
	EndLine   int         `json:"linea_fin,omitempty"`
	Function  string      `json:"funcion,omitempty"`
	Severity  Severity    `json:"severidad"`
	Origin    Origin      `json:"origen"`
	Group     Group       `json:"grupo"`
	What      string      `json:"que_pasa"`
	Why       string      `json:"por_que"`
	Fix       string      `json:"solucion"`
	FixStatus string      `json:"estado_solucion,omitempty"`
	Evidence  []string    `json:"evidencia"`
	Rule      *RuleRef    `json:"regla,omitempty"`
	Status    Status      `json:"estado"`
	Reason    string      `json:"motivo,omitempty"`
	Votes     []JudgeVote `json:"jueces,omitempty"`
}

// JudgeVote is one judge's answer for one finding.
type JudgeVote struct {
	Judge          string   `json:"juez"`
	FindingID      string   `json:"id"`
	Real           bool     `json:"real"`
	Worth          bool     `json:"vale_la_pena"`
	FixComplies    bool     `json:"solucion_cumple"`
	Evidence       []string `json:"evidencia"`
	RuleCited      string   `json:"regla_citada"`
	AlternativeFix string   `json:"solucion_alternativa"`
	Note           string   `json:"nota"`
}

// SplitPart is one proposed smaller PR. DependsOn is the 1-based index of the
// part it depends on, or 0 when independent.
type SplitPart struct {
	Title     string   `json:"titulo"`
	Files     []string `json:"archivos"`
	Lines     int      `json:"lineas"`
	DependsOn int      `json:"depende_de"`
}
