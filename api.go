package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
)

func serviceAPIRequests() {
	logger.Info("configuring API server")
	http.HandleFunc("/", handleHTTPRequest)

	logger.Info("starting API server")
	http.ListenAndServe("localhost:6669", nil)
}

func handleHTTPRequest(httpResponseWriter http.ResponseWriter, httpRequest *http.Request) {
	logger.Info("handling HTTP request")
	// for Requests:
	// - both POST and GET HTTP requests are supported
	// - we accept only on localhost (for security)
	// - the Request type (e.g. Version) can be a parameter (e.g. localhost:6669/?woodyRequestType=Version) or a header (e.g. "Woody-Request-Type=Version")
	// - Request parameters can be a parameter (e.g. localhost:6669/?woodyRequestType=Read8&woodyAddress=address) or a header (e.g. "Woody-Address=address")
	// - HTTP headers must always start with "Woody-" while URL parameters must always start with "woody"
	var pineRequestType string = ""
	var pineRequestParams map[string]string = make(map[string]string)
	// process the HTTP path parameters and headers
	err := httpRequest.ParseForm()
	if err != nil {
		errMessage := "could not parse HTTP form and/or path parameters"
		logger.Error(errMessage, "httpRequest", httpRequest)
		sendHTTPError(httpResponseWriter, 400, errMessage)
		return
	}
	var combinedRequestParams map[string][]string = make(map[string][]string)
	maps.Copy(combinedRequestParams, httpRequest.Form)
	maps.Copy(combinedRequestParams, httpRequest.Header)
	for combinedKey, combinedValue := range combinedRequestParams {
		// since HTTP headers and form parameters have different naming styles, we normalize on lowercase with no underscores
		adjustedKey := strings.ToLower(combinedKey)
		adjustedKey = strings.ReplaceAll(adjustedKey, "-", "")
		adjustedKey = strings.ReplaceAll(adjustedKey, "_", "")
		adjustedValue := combinedValue[0]

		// don't put the PINE Request type in the map
		if adjustedKey == "woodyrequesttype" {
			pineRequestType = strings.ToLower(adjustedValue)
		} else {
			pineRequestParams[adjustedKey] = adjustedValue
		}
	}
	if pineRequestType == "" {
		errMessage := "no PINE request type found in HTTP request"
		logger.Error(errMessage, "httpRequest", httpRequest)
		sendHTTPError(httpResponseWriter, 400, errMessage)
		return
	}

	handlePineRequest(httpResponseWriter, pineRequestType, pineRequestParams)
}

func sendHTTPError(httpResponseWriter http.ResponseWriter, statusCode int, errMessage string) {
	logger.Debug("sendHTTPError", "statusCode", statusCode)
	httpResponseWriter.Header().Set("Content-Type", "application/json")
	httpResponseWriter.WriteHeader(statusCode)
	jsonBytes, _ := json.Marshal(map[string]string{"errMessage": errMessage})
	// jsonString := fmt.Sprintf("{ \"errMessage\": \"%v\" }", errMessage)
	httpResponseWriter.Write(jsonBytes)
}

func handlePineRequest(httpResponseWriter http.ResponseWriter, pineRequestType string, pineRequestParams map[string]string) {
	logger.Info("processing the PINE request", "pineRequestType", pineRequestType, "pineRequestParams", pineRequestParams)

	// for Answers:
	// - if the Request type is unknown a 400 (Bad Request) HTTP response code is returned
	// - we map the result code to a corresponding HTTP response code:
	//   - result code 00 maps to a 200 (OK) HTTP response code
	//   - result code FF maps to a 500 (Internal Server Error) HTTP response code
	//   - other result code map to a 501 (Not Implemented) HTTP response code
	// - the HTTP response is a JSON document where any Answer parameters are returned

	// parse the parameters for the request
	var address uint32
	var dataUInt64 uint64
	var width int
	var slot uint8
	switch pineRequestType {
	case "read8", "read16", "read32", "read64", "write8", "write16", "write32", "write64":
		addressString, found := pineRequestParams["woodyaddress"]
		logger.Debug("parsing parameters for read/write", "addressString", addressString, "found", found)
		if !found {
			errMessage := "no address provided for " + pineRequestType + " PINE request"
			logger.Error(errMessage)
			sendHTTPError(httpResponseWriter, 400, errMessage)
			return
		}
		addressUInt64, err := parseInt(addressString, 32)
		logger.Debug("parsing parameters for read/write", "addressUInt64", addressUInt64, "err", err)
		if err != nil {
			errMessage := "unable to parse address " + addressString + " for " + pineRequestType + " PINE request"
			logger.Error(errMessage)
			sendHTTPError(httpResponseWriter, 400, errMessage)
			return
		}
		address = uint32(addressUInt64)

		// for write requests, we also need the data
		if strings.HasPrefix(pineRequestType, "write") {
			dataString, found := pineRequestParams["woodydata"]
			if !found {
				errMessage := "no data provided for " + pineRequestType + " PINE request"
				logger.Error(errMessage)
				sendHTTPError(httpResponseWriter, 400, errMessage)
				return
			}
			widthInt64, _ := strconv.ParseInt(strings.TrimPrefix(pineRequestType, "write"), 10, 8)
			width = int(widthInt64)
			dataUInt64, err = parseInt(dataString, width)
			if err != nil {
				errMessage := "unable to parse data " + dataString + " for " + pineRequestType + " PINE request"
				logger.Error(errMessage)
				sendHTTPError(httpResponseWriter, 400, errMessage)
				return
			}
		}
	case "savestate", "loadstate":
		slotString, found := pineRequestParams["woodyslot"]
		if !found {
			errMessage := "no slot provided for " + pineRequestType + " PINE request"
			logger.Error(errMessage)
			sendHTTPError(httpResponseWriter, 400, errMessage)
			return
		}
		slotUInt64, err := parseInt(slotString, 8)
		if err != nil {
			errMessage := "unable to parse slot " + slotString + " for " + pineRequestType + " PINE request"
			logger.Error(errMessage)
			sendHTTPError(httpResponseWriter, 400, errMessage)
			return
		}
		slot = uint8(slotUInt64)
	}
	logger.Debug("after parsing the parameters for the request", "address", address, "dataUInt64", dataUInt64, "width", width, "slot", slot)

	// create and send the request
	var requestBytes []byte
	var err error
	switch pineRequestType {
	case "read8":
		requestBytes, err = PineRead8Request{address: address}.toBytes()
	case "read16":
		requestBytes, err = PineRead16Request{address: address}.toBytes()
	case "read32":
		requestBytes, err = PineRead32Request{address: address}.toBytes()
	case "read64":
		requestBytes, err = PineRead64Request{address: address}.toBytes()
	case "write8":
		requestBytes, err = PineWrite8Request{address: address, data: uint8(dataUInt64)}.toBytes()
	case "write16":
		requestBytes, err = PineWrite16Request{address: address, data: uint16(dataUInt64)}.toBytes()
	case "write32":
		requestBytes, err = PineWrite32Request{address: address, data: uint32(dataUInt64)}.toBytes()
	case "write64":
		requestBytes, err = PineWrite64Request{address: address, data: uint64(dataUInt64)}.toBytes()
	case "version":
		requestBytes, err = PineVersionRequest{}.toBytes()
	case "savestate":
		requestBytes, err = PineSaveStateRequest{slot: slot}.toBytes()
	case "loadstate":
		requestBytes, err = PineLoadStateRequest{slot: slot}.toBytes()
	case "title":
		requestBytes, err = PineTitleRequest{}.toBytes()
	case "id":
		requestBytes, err = PineIDRequest{}.toBytes()
	case "uuid":
		requestBytes, err = PineUUIDRequest{}.toBytes()
	case "gameversion":
		requestBytes, err = PineGameVersionRequest{}.toBytes()
	case "status":
		requestBytes, err = PineStatusRequest{}.toBytes()
	default:
		errMessage := "unknown request type when creating requestBytes for " + pineRequestType + " PINE request"
		logger.Error(errMessage)
		sendHTTPError(httpResponseWriter, 400, errMessage)
		return
	}
	if err != nil {
		errMessage := "error while creating requestBytes for " + pineRequestType + " PINE request"
		logger.Error(errMessage, "err", err)
		sendHTTPError(httpResponseWriter, 400, errMessage)
		return
	}
	answerBytes, err := pc.Send(requestBytes)
	if err != nil {
		errMessage := "error while sending requestBytes for " + pineRequestType + " PINE request"
		logger.Error(errMessage, "err", err)
		sendHTTPError(httpResponseWriter, 400, errMessage)
		return
	}

	// convert the bytes into an Answer struct then convert that into JSON
	var fromBytesErr error
	var jsonString string
	var resultCode uint8
	switch pineRequestType {
	case "read8":
		var answer *PineRead8Answer = &PineRead8Answer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"memoryValue\": %v }", answer.resultCode, answer.memoryValue)
		}
	case "read16":
		var answer *PineRead16Answer = &PineRead16Answer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"memoryValue\": %v }", answer.resultCode, answer.memoryValue)
		}
	case "read32":
		var answer *PineRead32Answer = &PineRead32Answer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"memoryValue\": %v }", answer.resultCode, answer.memoryValue)
		}
	case "read64":
		var answer *PineRead64Answer = &PineRead64Answer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"memoryValue\": %v }", answer.resultCode, answer.memoryValue)
		}
	case "write8":
		var answer *PineWrite8Answer = &PineWrite8Answer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v }", answer.resultCode)
		}
	case "write16":
		var answer *PineWrite16Answer = &PineWrite16Answer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v }", answer.resultCode)
		}
	case "write32":
		var answer *PineWrite32Answer = &PineWrite32Answer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v }", answer.resultCode)
		}
	case "write64":
		var answer *PineWrite64Answer = &PineWrite64Answer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v }", answer.resultCode)
		}
	case "version":
		var answer *PineVersionAnswer = &PineVersionAnswer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"version\": \"%v\" }", answer.resultCode, answer.version)
		}
	case "savestate":
		var answer *PineSaveStateAnswer = &PineSaveStateAnswer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v }", answer.resultCode)
		}
	case "loadstate":
		var answer *PineLoadStateAnswer = &PineLoadStateAnswer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v }", answer.resultCode)
		}
	case "title":
		var answer *PineTitleAnswer = &PineTitleAnswer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"title\": \"%v\" }", answer.resultCode, answer.title)
		}
	case "id":
		var answer *PineIDAnswer = &PineIDAnswer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"id\": \"%v\" }", answer.resultCode, answer.id)
		}
	case "uuid":
		var answer *PineUUIDAnswer = &PineUUIDAnswer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"uuid\": \"%v\" }", answer.resultCode, answer.uuid)
		}
	case "gameversion":
		var answer *PineGameVersionAnswer = &PineGameVersionAnswer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			logger.Debug("blarg", "len(answer.gameVersion)", len(answer.gameVersion))
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"gameVersion\": \"%v\" }", answer.resultCode, answer.gameVersion)
		}
	case "status":
		var answer *PineStatusAnswer = &PineStatusAnswer{}
		fromBytesErr = answer.fromBytes(answerBytes)
		if fromBytesErr == nil {
			resultCode = answer.resultCode
			jsonString = fmt.Sprintf("{ \"resultCode\": %v, \"status\": \"%v\" }", answer.resultCode, answer.status)
		}
	default:
		errMessage := "unknown request type when creating Answer struct for " + pineRequestType + " PINE request"
		logger.Error(errMessage)
		sendHTTPError(httpResponseWriter, 400, errMessage)
		return
	}
	if fromBytesErr != nil {
		errMessage := "error while converting answerBytes to Answer struct for " + pineRequestType + " PINE request"
		logger.Error(errMessage, "err", err, "answerBytes", answerBytes)
		sendHTTPError(httpResponseWriter, 400, errMessage)
		return
	}

	// send the HTTP response
	var statusCode int
	if resultCode == 0 {
		statusCode = 200
	} else if resultCode == 255 {
		statusCode = 500
	} else {
		statusCode = 501
	}
	logger.Debug("when building the response body", "jsonString", jsonString)
	httpResponseWriter.Header().Set("Content-Type", "application/json")
	httpResponseWriter.WriteHeader(statusCode)
	httpResponseWriter.Write([]byte(jsonString))
}

func parseInt(num string, bitSize int) (uint64, error) {
	if strings.Index(num, "0x") == 0 {
		return strconv.ParseUint(strings.TrimPrefix(num, "0x"), 16, bitSize)
	} else {
		return strconv.ParseUint(num, 10, bitSize)
	}
}
