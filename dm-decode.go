package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	"github.com/dfuse-io/dfuse-eosio/codec"
	pbcodec "github.com/dfuse-io/dfuse-eosio/pb/dfuse/eosio/codec/v1"
	"github.com/dfuse-io/logging"
	"github.com/golang/protobuf/ptypes"
	pbts "github.com/golang/protobuf/ptypes/timestamp"
	"github.com/streamingfast/jsonpb"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

var fixedTimestamp *pbts.Timestamp
var zlog = zap.NewNop()

func init() {
	if os.Getenv("DEBUG") != "" {
		zlog, _ = zap.NewDevelopment()
		logging.Override(zlog)
	}

	fixedTime, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	fixedTimestamp, _ = ptypes.TimestampProto(fixedTime)
}

func main() {

	ensure(len(os.Args) == 2, "Single argument must be <path to file> to decode")

	actualDmlogFile := os.Args[1];
	actualJSONFile := actualDmlogFile + ".json"

	actualBlocks := readActualBlocks(actualDmlogFile)
	zlog.Info("read all blocks from dmlog file", zap.Int("block_count", len(actualBlocks)), zap.String("file", actualDmlogFile))

	writeActualBlocks(actualJSONFile, actualBlocks)
	
	fmt.Printf("Done! Saved JSON to %s", actualJSONFile)
	os.Exit(0)
}

func writeActualBlocks(actualFile string, blocks []*pbcodec.Block) {
	file, err := os.Create(actualFile)
	noError(err, "Unable to write file %q", actualFile)
	defer file.Close()

	_, err = file.WriteString("[\n")
	noError(err, "Unable to write list start")

	blockCount := len(blocks)
	if blockCount > 0 {
		lastIndex := blockCount - 1
		for i, block := range blocks {
			out, err := jsonpb.MarshalIndentToString(block, "  ")
			noError(err, "Unable to marshal block %q", block.AsRef())

			_, err = file.WriteString(out)
			noError(err, "Unable to write block %q", block.AsRef())

			if i != lastIndex {
				_, err = file.WriteString(",\n")
				noError(err, "Unable to write block delimiter %q", block.AsRef())
			}
		}
	}

	_, err = file.WriteString("]\n")
	noError(err, "Unable to write list end")
}

func readActualBlocks(filePath string) []*pbcodec.Block {
	blocks := []*pbcodec.Block{}

	file, err := os.Open(filePath)
	noError(err, "Unable to open actual blocks file %q", filePath)
	defer file.Close()

	reader, err := codec.NewConsoleReader(file)
	noError(err, "Unable to create console reader for actual blocks file %q", filePath)
	defer reader.Close()

	var lastBlockRead *pbcodec.Block
	for {
		el, err := reader.Read()
		if el != nil && el.(*pbcodec.Block) != nil {
			block, ok := el.(*pbcodec.Block)
			ensure(ok, `Read block is not a "pbcodec.Block" but should have been`)

			lastBlockRead = sanitizeBlock(block)
			fmt.Printf("Parsed block %q\n", lastBlockRead.AsRef())
			blocks = append(blocks, lastBlockRead)
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			if lastBlockRead == nil {
				noError(err, "Unable to read first block from file %q", filePath)
			} else {
				noError(err, "Unable to read block from file %q, last block read was %s\n", filePath, lastBlockRead.AsRef())
			}
		}
	}

	return blocks
}

func sanitizeBlock(block *pbcodec.Block) *pbcodec.Block {
	var sanitizeContext func(logContext *pbcodec.Exception_LogContext)
	sanitizeContext = func(logContext *pbcodec.Exception_LogContext) {
		if logContext != nil {
			logContext.Line = 666
			logContext.ThreadName = "thread"
			logContext.Timestamp = fixedTimestamp
			sanitizeContext(logContext.Context)
		}
	}

	sanitizeException := func(exception *pbcodec.Exception) {
		if exception != nil {
			for _, stack := range exception.Stack {
				sanitizeContext(stack.Context)
			}
		}
	}

	sanitizeRLimitOp := func(rlimitOp *pbcodec.RlimitOp) {
		switch v := rlimitOp.Kind.(type) {
		case *pbcodec.RlimitOp_AccountUsage:
			v.AccountUsage.CpuUsage.LastOrdinal = 111
			v.AccountUsage.NetUsage.LastOrdinal = 222
		case *pbcodec.RlimitOp_State:
			v.State.AverageBlockCpuUsage.LastOrdinal = 333
			v.State.AverageBlockNetUsage.LastOrdinal = 444
		}
	}

	for _, rlimitOp := range block.RlimitOps {
		sanitizeRLimitOp(rlimitOp)
	}

	for _, trxTrace := range block.UnfilteredTransactionTraces {
		trxTrace.Elapsed = 888
		sanitizeException(trxTrace.Exception)

		for _, permOp := range trxTrace.PermOps {
			if permOp.OldPerm != nil {
				permOp.OldPerm.LastUpdated = fixedTimestamp
			}

			if permOp.NewPerm != nil {
				permOp.NewPerm.LastUpdated = fixedTimestamp
			}
		}

		for _, rlimitOp := range trxTrace.RlimitOps {
			sanitizeRLimitOp(rlimitOp)
		}

		for _, actTrace := range trxTrace.ActionTraces {
			actTrace.Elapsed = 999
			sanitizeException(actTrace.Exception)
		}

		if trxTrace.FailedDtrxTrace != nil {
			sanitizeException(trxTrace.FailedDtrxTrace.Exception)
			for _, actTrace := range trxTrace.FailedDtrxTrace.ActionTraces {
				sanitizeException(actTrace.Exception)
			}
		}
	}

	return block
}

func jsonEq(expectedFile string, actualFile string) bool {
	expected, err := ioutil.ReadFile(expectedFile)
	noError(err, "Unable to read %q", expectedFile)

	actual, err := ioutil.ReadFile(actualFile)
	noError(err, "Unable to read %q", actualFile)

	var expectedJSONAsInterface, actualJSONAsInterface interface{}

	err = json.Unmarshal(expected, &expectedJSONAsInterface)
	noError(err, "Expected file %q is not a valid JSON file", expectedFile)

	err = json.Unmarshal(actual, &actualJSONAsInterface)
	noError(err, "Actual file %q is not a valid JSON file", actualFile)

	return assert.ObjectsAreEqualValues(expectedJSONAsInterface, actualJSONAsInterface)
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		// For this script, we don't care
		return false
	}

	return !stat.IsDir()
}

func ensure(condition bool, message string, args ...interface{}) {
	if !condition {
		quit(message, args...)
	}
}

func noError(err error, message string, args ...interface{}) {
	if err != nil {
		quit(message+": "+err.Error(), args...)
	}
}

func quit(message string, args ...interface{}) {
	fmt.Printf(message+"\n", args...)
	os.Exit(1)
}
