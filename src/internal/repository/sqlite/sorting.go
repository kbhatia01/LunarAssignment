package sqlite

import "lunar-rockets/src/internal/service/repository"

func orderBy(options repository.ListRocketsOptions) string {
	columns := map[string]string{
		repository.SortChannel:           "channel",
		repository.SortMission:           "mission",
		repository.SortSpeed:             "speed",
		repository.SortStatus:            "status",
		repository.SortLastMessageNumber: "last_message_number",
	}
	column := columns[options.Sort]
	if column == "" {
		column = "channel"
	}
	direction := "ASC"
	if options.Order == repository.OrderDesc {
		direction = "DESC"
	}
	if column == "channel" {
		return "channel " + direction
	}
	return column + " " + direction + ", channel ASC"
}
