package main

import (
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"log"
	"os"
)

var dumpDB *sqlx.DB

type Icon struct {
	ID     int    `db:"id"`
	UserID int    `db:"user_id"`
	Image  []byte `db:"image"`
}

func dumpImage(icon *Icon) error {
	// 画像
	img := icon.Image

	// ファイルに書き出す
	imageFilePath := fmt.Sprintf("icon/%d", icon.UserID)
	err := os.WriteFile(imageFilePath, img, 0644)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	host := "node2"
	port := "3306"
	user := "isucon"
	password := "isucon"
	dbname := "isupipe"

	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		user,
		password,
		host,
		port,
		dbname,
	)

	dumpDB, err := sqlx.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %s.", err.Error())
	}

	// iconsテーブルをすべて取得
	icons := make([]Icon, 0)
	err = dumpDB.Select(&icons, "SELECT * FROM icons")
	if err != nil {
		log.Fatalf("Failed to select icons: %s.", err.Error())
	}

	// 画像をすべてファイルに書き出す
	for _, icon := range icons {
		err := dumpImage(&icon)
		if err != nil {
			log.Fatalf("Failed to dump image: %s.", err.Error())
		}
	}

	defer dumpDB.Close()

}
