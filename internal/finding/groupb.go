package finding

// FilterGroupB keeps every non-B finding and only those Group B findings that
// share a file, or a function within a file, with a confirmed Group A finding.
func FilterGroupB(fs []Finding) []Finding {
	files := map[string]bool{}
	funcs := map[[2]string]bool{}
	for _, f := range fs {
		if f.Group != GroupA || f.Status != StatusConfirmado {
			continue
		}
		files[f.File] = true
		if f.Function != "" {
			funcs[[2]string{f.File, f.Function}] = true
		}
	}

	out := make([]Finding, 0, len(fs))
	for _, f := range fs {
		if f.Group != GroupB || files[f.File] ||
			(f.Function != "" && funcs[[2]string{f.File, f.Function}]) {
			out = append(out, f)
		}
	}
	return out
}
