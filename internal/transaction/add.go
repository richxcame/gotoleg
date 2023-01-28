package transaction

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"gotoleg/internal/constants"
	"gotoleg/internal/db"
	"gotoleg/internal/utility"
	"gotoleg/pkg/hmacsha1"
	"gotoleg/pkg/logger"
	pb "gotoleg/rpc/gotoleg"
)

var DB *sql.DB

func init() {
	db, err := db.SetupDatabase()
	if err != nil {
		logger.Fatal("database connection error: %v", err.Error())
	}
	DB = db
}

// Add sends money to given destination
//
// POST /api/<username>/<server>/txn/add
// local-id - client id of transaction (max 30 chars)
// service - service key (can be received from 0.3. Services Request)
// amount - transaction amount in cents (100 for 1 manat)
// destination - msisdn (no country code for mts and tmcell)
// txn-ts - epoch time of transaction
// ts - epoch time at client
// hmac - hmac with access token
//
// msg = <local-id>:<service>:<amount>:<destination>:<txn-ts>:<ts>:<username>
func (s *Server) Add(ctx context.Context, in *pb.TransactionRequest) (*pb.TransactionReply, error) {
	// Get epoch time
	epochTime, err := utility.GetEpoch()
	if err != nil {
		logger.Error(err)
		return nil, err
	}

	// Prepare ts, msg and request body
	ts := strconv.FormatInt(epochTime, 10)
	msg := fmt.Sprintf("%s:%s:%s:%s:%s:%s:%s", in.LocalID, in.Service, in.Amount, in.Phone, ts, ts, constants.USERNAME)
	data := url.Values{
		"local-id":    {in.LocalID},
		"service":     {in.Service},
		"amount":      {in.Amount},
		"destination": {in.Phone},
		"txn-ts":      {ts},
		"ts":          {ts},
		"hmac":        {hmacsha1.Generate(constants.ACCESS_TOKEN, msg)},
	}

	resp, err := http.PostForm(constants.ADD_TRANSACTION_URL, data)
	if err != nil {
		logger.Error(err)
		return nil, err
	}
	defer resp.Body.Close()

	respInBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error(err)
		return nil, err
	}

	var result GarynjaResponse
	err = json.Unmarshal(respInBytes, &result)
	if err != nil {
		logger.Error(err)
		return nil, err
	}

	// Save the result to database
	const sqlStatement = `INSERT INTO transactions (id, local_id, service, phone, status, error_code, error_msg, result_status, result_ref_num, result_service, result_destination, result_amount, result_state) 
					VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err = DB.Exec(sqlStatement, result.Status, result.ErrorCode, result.ErrorMessage, result.Result.Status, result.Result.RefNum, result.Result.Service, result.Result.Destination, result.Result.Amount, result.Result.State)
	if err != nil {
		logger.Errorf("database.insert.error", in.LocalID, result)
	}

	return &pb.TransactionReply{
		Status:       result.Status,
		ErrorCode:    result.ErrorCode,
		ErrorMessage: result.ErrorMessage,
		Result: &pb.Result{
			Status:      result.Result.Status,
			RefNum:      result.Result.RefNum,
			Service:     result.Result.Service,
			Destination: result.Result.Destination,
			Amount:      result.Result.Amount,
			State:       result.Result.State,
		},
	}, nil
}
