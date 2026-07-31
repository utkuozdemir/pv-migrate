package rclone

// exitCodeMeanings are the rclone exit values worth naming, as its documentation
// words them. The table is deliberately short: rclone puts almost everything
// under codes 2 and 7, which say no more than "an error", and the job runs with
// --use-json-log, so the log tail is where the diagnosis actually is. Code 1 is
// left out on the same evidence: rclone was observed exiting 1 for a credentials
// failure, so quoting its documented "syntax or usage error" would mislead.
// Codes with nothing distinct to say, including ones rclone adds later, get the
// bare number and let the tail speak.
var exitCodeMeanings = map[int]string{
	3: "Directory not found",
	4: "File not found",
}

// Interpret returns what rclone's documentation says about an exit status, or an
// empty string when there is nothing distinct to say about it.
func Interpret(code int) string {
	meaning, ok := exitCodeMeanings[code]
	if !ok {
		return ""
	}

	return "rclone documents this exit code as: " + meaning
}
