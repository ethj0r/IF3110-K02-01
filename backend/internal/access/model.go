package Access

type DatabaseAccess struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Host   string `json:"host"`
	DBName string `json:"dbname"`
	Level  Level  `json:"level"`
}
