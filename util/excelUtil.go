package util

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/tealeg/xlsx"
)

const layout = "2006-01-02 15:04:05"

func WritePayments(records []map[string]interface{}, writer io.Writer) {
	var file *xlsx.File
	var sheet *xlsx.Sheet
	var row *xlsx.Row
	var cell *xlsx.Cell
	var err error

	file = xlsx.NewFile()
	sheet, err = file.AddSheet("Sheet1")
	if err != nil {
		fmt.Printf(err.Error())
	}
	row = sheet.AddRow()
	row.SetHeightCM(1)
	cell = row.AddCell()
	cell.Value = "Adress"
	cell = row.AddCell()
	cell.Value = "Amount"
	cell = row.AddCell()
	cell.Value = "Time"
	cell = row.AddCell()
	cell.Value = "Tx"
	for _, r := range records {
		row = sheet.AddRow()
		row.SetHeightCM(1)
		cell = row.AddCell()
		cell.Value = r["address"].(string)
		cell = row.AddCell()
		ammout := "0"
		if _, ok := r["amount"]; ok {
			ammout = strconv.FormatInt(r["amount"].(int64), 10)
		}
		cell.Value = ammout
		cell = row.AddCell()
		timestamp := r["timestamp"].(int64)
		timeU := time.Unix(timestamp, 0)
		cell.Value = timeU.Format(layout)
		cell = row.AddCell()
		cell.Value = r["tx"].(string)
	}
	err = file.Write(writer)
	if err != nil {
		fmt.Printf(err.Error())
	}

}

func GetBeginOfDay(date string) int64 {
	timeStr := date + " 00:00:00"
	t, _ := time.Parse(layout, timeStr)
	return t.Unix()

}
func GetEndOfDay(date string) int64 {
	timeStr := date + " 23:59:59"
	t, _ := time.Parse(layout, timeStr)
	return t.Unix()

}

func timeSub(t1, t2 time.Time) int {
	t1 = time.Date(t1.Year(), t1.Month(), t1.Day(), 0, 0, 0, 0, time.Local)
	t2 = time.Date(t2.Year(), t2.Month(), t2.Day(), 0, 0, 0, 0, time.Local)

	return int(t1.Sub(t2).Hours() / 24)
}

func DaySub(from, to string) int {
	from = from + " 00:00:00"
	to = to + " 23:59:59"
	t1, _ := time.Parse(layout, from)
	t2, _ := time.Parse(layout, to)
	return timeSub(t2, t1)
}
