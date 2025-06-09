package validate

import "regexp"

func CheckTitleFormat(title string) (string, string, string, bool) {
	re := regexp.MustCompile(`^\[(NEW|UPDATE|SUSPEND|REMOVE)\]\[([a-z0-9]{1,30})\]\[(\d+\.\d+\.\d+)\].*$`)
	match := re.FindStringSubmatch(title)
	if len(match) != 4 {
		return "", "", "", false
	}

	prType := match[1]
	folderName := match[2]
	submitVersion := match[3]

	return prType, folderName, submitVersion, true
}
