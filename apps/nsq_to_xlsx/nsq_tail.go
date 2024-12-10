package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nsqio/go-nsq"
	"github.com/nsqio/nsq/internal/app"
	"github.com/nsqio/nsq/internal/version"
	"github.com/xuri/excelize/v2"
)

var (
	showVersion = flag.Bool("version", false, "print version string")

	channel       = flag.String("channel", "", "NSQ channel")
	maxInFlight   = flag.Int("max-in-flight", 200, "max number of messages to allow in flight")
	totalMessages = flag.Int("n", 10, "total messages to show (will wait if starved)")
	printTopic    = flag.Bool("print-topic", false, "print topic name where message was received")

	nsqdTCPAddrs     = app.StringArray{}
	lookupdHTTPAddrs = app.StringArray{}
	topics           = app.StringArray{}
)

func init() {
	flag.Var(&nsqdTCPAddrs, "nsqd-tcp-address", "nsqd TCP address (may be given multiple times)")
	flag.Var(&lookupdHTTPAddrs, "lookupd-http-address", "lookupd HTTP address (may be given multiple times)")
	flag.Var(&topics, "topic", "NSQ topic (may be given multiple times)")
}

type TailHandler struct {
	topicName     string
	totalMessages int
	messagesShown int
	xlsxFile      *excelize.File
	lines         int
}

func (th *TailHandler) HandleMessage(m *nsq.Message) error {
	th.messagesShown++

	if *printTopic {
		_, err := os.Stdout.WriteString(th.topicName)
		if err != nil {
			log.Fatalf("ERROR: failed to write to os.Stdout - %s", err)
		}
		_, err = os.Stdout.WriteString(" | ")
		if err != nil {
			log.Fatalf("ERROR: failed to write to os.Stdout - %s", err)
		}
	}

	var rows [1024][8]float64
	err := binary.Read(bytes.NewReader(m.Body), binary.LittleEndian, &rows)
	if err != nil {
		return err
	}
	row := make([]interface{}, 8)
	for i := 0; i < 1024; i++ {
		for j := 0; j < 8; j++ {
			row[j] = rows[i][j]
		}
		cell, err := excelize.CoordinatesToCellName(1, th.lines)
		if err != nil {
			fmt.Println(err)
			break
		}
		if err := th.xlsxFile.SetSheetRow("Sheet1", cell, &row); err != nil {
			fmt.Println(err)
			break
		}
		th.lines = th.lines + 1
	}

	if th.totalMessages > 0 && th.messagesShown >= th.totalMessages {
		cell, err := excelize.CoordinatesToCellName(1, 1)
		if err != nil {
			fmt.Println(err)
		}
		for j := 0; j < 8; j++ {
			row[j] = fmt.Sprintf("通道%d", j)
		}

		if err := th.xlsxFile.SetSheetRow("Sheet1", cell, &row); err != nil {
			fmt.Println(err)
		}
		if err := th.xlsxFile.AddChart("Sheet1", "I1", &excelize.Chart{
			Type: excelize.Line,
			Series: []excelize.ChartSeries{
				{
					Name:   "Sheet1!$A$1",
					Values: "Sheet1!$A$2:$A$2000",
				},
				{
					Name:   "Sheet1!$B$1",
					Values: "Sheet1!$B$2:$B$2000",
				},
				{
					Name:   "Sheet1!$C$1",
					Values: "Sheet1!$C$1:$C$2000",
				},
				{
					Name:   "Sheet1!$D$1",
					Values: "Sheet1!$D$2:$D$2000",
				},
				{
					Name:   "Sheet1!$E$1",
					Values: "Sheet1!$E$2:$E$2000",
				},
				{
					Name:   "Sheet1!$F$1",
					Values: "Sheet1!$F$1:$F$2000",
				},
				{
					Name:   "Sheet1!$G$1",
					Values: "Sheet1!$G$2:$G$2000",
				},
				{
					Name:   "Sheet1!$H$1",
					Values: "Sheet1!$H$1:$H$2000",
				}},
			Title: []excelize.RichTextRun{
				{
					Text: "采集通道数据汇总",
				},
			},
		}); err != nil {
			fmt.Println(err)
		}
		fmt.Println(nsqdTCPAddrs[0])

		ipstr := strings.TrimSuffix(nsqdTCPAddrs[0], ":4150")
		fmt.Println(ipstr)
		fileName := strings.Replace(ipstr, ".", "_", -1)
		fileName += ".xlsx"
		fmt.Println(fileName)
		if err := th.xlsxFile.SaveAs(fileName); err != nil {
			fmt.Println(err)
		}
		os.Exit(0)
	}
	return nil
}

func main() {
	cfg := nsq.NewConfig()

	flag.Var(&nsq.ConfigFlag{cfg}, "consumer-opt", "option to passthrough to nsq.Consumer (may be given multiple times, http://godoc.org/github.com/nsqio/go-nsq#Config)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("nsq_tail v%s\n", version.Binary)
		return
	}

	if *channel == "" {
		rand.Seed(time.Now().UnixNano())
		*channel = fmt.Sprintf("tail%06d#ephemeral", rand.Int()%999999)
	}

	if len(nsqdTCPAddrs) == 0 && len(lookupdHTTPAddrs) == 0 {
		log.Fatal("--nsqd-tcp-address or --lookupd-http-address required")
	}
	if len(nsqdTCPAddrs) > 0 && len(lookupdHTTPAddrs) > 0 {
		log.Fatal("use --nsqd-tcp-address or --lookupd-http-address not both")
	}
	if len(topics) == 0 {
		log.Fatal("--topic required")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Don't ask for more messages than we want
	if *totalMessages > 0 && *totalMessages < *maxInFlight {
		*maxInFlight = *totalMessages
	}

	xlsxFile := excelize.NewFile()
	defer func() {
		if err := xlsxFile.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	// styleID, err := xlsxFile.NewStyle(&excelize.Style{Font: &excelize.Font{Color: "777777"}})
	// if err != nil {
	// 	fmt.Println(err)
	// 	return
	// }
	// 流式设置单元格的公式和值：
	// if err := sw.SetRow("A1",
	// 	[]interface{}{
	// 		excelize.Cell{StyleID: styleID, Value: "Data"},
	// 		[]excelize.RichTextRun{
	// 			{Text: "Rich ", Font: &excelize.Font{Color: "2354e8"}},
	// 			{Text: "Text", Font: &excelize.Font{Color: "e83723"}},
	// 		},
	// 	},
	// 	// 流式设置单元格的值和行样式：
	// 	excelize.RowOpts{Height: 45, Hidden: false}); err != nil {
	// 	fmt.Println(err)
	// 	return
	// }
	// if err := sw.SetRow("A1",
	// 	[]interface{}{
	// 		"通道0",
	// 		"通道1",
	// 		"通道2",
	// 		"通道3",
	// 		"通道4",
	// 		"通道5",
	// 		"通道6",
	// 		"通道7",
	// 	}); err != nil {
	// 	fmt.Println(err)
	// 	return
	// }

	cfg.UserAgent = fmt.Sprintf("nsq_tail/%s go-nsq/%s", version.Binary, nsq.VERSION)
	cfg.MaxInFlight = *maxInFlight

	consumers := []*nsq.Consumer{}
	for i := 0; i < len(topics); i += 1 {
		log.Printf("Adding consumer for topic: %s\n", topics[i])

		consumer, err := nsq.NewConsumer(topics[i], *channel, cfg)
		if err != nil {
			log.Fatal(err)
		}

		consumer.AddHandler(&TailHandler{topicName: topics[i], totalMessages: *totalMessages, xlsxFile: xlsxFile, lines: 2})

		err = consumer.ConnectToNSQDs(nsqdTCPAddrs)
		if err != nil {
			log.Fatal(err)
		}

		err = consumer.ConnectToNSQLookupds(lookupdHTTPAddrs)
		if err != nil {
			log.Fatal(err)
		}

		consumers = append(consumers, consumer)
	}

	<-sigChan

	for _, consumer := range consumers {
		consumer.Stop()
	}
	for _, consumer := range consumers {
		<-consumer.StopChan
	}
}
