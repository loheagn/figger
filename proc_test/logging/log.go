package logging

import "log"

func Error(format string, v ...any) {
	format = "ERROR " + format
	log.Printf(format, v...)
}
