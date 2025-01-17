package main
// #include "wapp.h"
/*

   #include <stdlib.h>

   typedef void (*ptr_to_python_function) (char*);

   static inline void call_c_func(ptr_to_python_function ptr, char* jsonStr) {
     (ptr)(jsonStr);
   }
*/
import "C"

import (
    "os"
    "net/http"
    // "os/signal"
    // "syscall"
    "path/filepath"
    "fmt"
    "context"
    // "github.com/mdp/qrterminal/v3"
    "go.mau.fi/whatsmeow"
    waProto "go.mau.fi/whatsmeow/binary/proto"
    "go.mau.fi/whatsmeow/store/sqlstore"
    "go.mau.fi/whatsmeow/types"
    "go.mau.fi/whatsmeow/types/events"    
    waLog "go.mau.fi/whatsmeow/util/log"
    "go.mau.fi/whatsmeow/store"
    "google.golang.org/protobuf/proto"
    _ "modernc.org/sqlite"
    // sqlite3 "github.com/mattn/go-sqlite3"

    "strings"
    // "mime"
    "time"
    "sync/atomic"
    "encoding/json"
    "go.mau.fi/whatsmeow/appstate"
    "github.com/enriquebris/goconcurrentqueue"
    "unsafe"
    "strconv"
    "google.golang.org/protobuf/encoding/protojson"
)

// var log waLog.Logger
var historySyncID int32
var startupTime = time.Now().Unix()

var WpClient *whatsmeow.Client
var EventQueue = goconcurrentqueue.NewFIFO()


//export Connect
func Connect() {
    // Set the path for the database file
    dbPath := "whatsapp/wapp.db"

    // Set Browser
    store.DeviceProps.PlatformType = waProto.DeviceProps_SAFARI.Enum()
    store.DeviceProps.Os = proto.String("macOS") //"Mac OS 10"

    // Create the directory if it doesn't exist
    err := os.MkdirAll(filepath.Dir(dbPath), 0755)
    if err != nil {
        panic(err)
    }

    // Connect to the database
    container, err := sqlstore.New("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)", waLog.Noop)
    if err != nil {
        panic(err)
    }

    deviceStore, err := container.GetFirstDevice()
    if err != nil {
        panic(err)
    }
    client := whatsmeow.NewClient(deviceStore, waLog.Noop)

    if client.Store.ID == nil {
        // No ID stored, new login
        qrChan, _ := client.GetQRChannel(context.Background())
        err = client.Connect()
        if err != nil {
            panic(err)
        }
        for {
            select {
            case <-time.After(60 * time.Second):
                client.Disconnect()
                fmt.Println("Timeout; disconnect")
                return
            case evt, ok := <-qrChan:
                if !ok {
                    return
                }
                if evt.Event == "code" {
                    // qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
                    fmt.Println("QR code:", evt.Code)
                } else {
                    fmt.Println("Login event:", evt.Event)
                }
            }
        }
    } else {
        client.AddEventHandler(handler)
        err := client.Connect()
        fmt.Println("User already logged in")
        if err != nil {
            panic(err)
        }
    }

    WpClient = client
    // Listen to Ctrl+C (you can also do something else that prevents the program from exiting)
    // c := make(chan os.Signal)
    // signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    // <-c
}

//export Disconnect
func Disconnect() {
    WpClient.Disconnect()
    fmt.Println("Disconnected")
}


func assignUserJid(number string) types.JID {
    jid := types.JID{
        User:   number,
        Server: types.DefaultUserServer,
    }
    return jid
}

func assignGroupJid(number string) types.JID {
    jid := types.JID{
        User:   number,
        Server: types.GroupServer,
    }
    return jid
}

func _SendMessage(number types.JID, msg *C.char) C.int {
    // safely reset the msg string. there is a concat issue
    message := &waProto.Message{
        Conversation: proto.String(""),
    }
    message.Conversation = proto.String(C.GoString(msg))

    // Check if the client is connected
    if !WpClient.IsConnected() {
        fmt.Println("Reconnect")
        err := WpClient.Connect()
        if err != nil {
            return 1
        }
    }
    
    _, err := WpClient.SendMessage(context.Background(), number, message)
    if err != nil {
        return 1
    }
    return 0
}

//export SendMessage
func SendMessage(number *C.char, msg *C.char) C.int {
    var jid types.JID = assignUserJid(C.GoString(number))
    return _SendMessage(jid, msg)
}

//export SendGroupMessage
func SendGroupMessage(number *C.char, msg *C.char) C.int {
    var jid types.JID = assignGroupJid(C.GoString(number))
    return _SendMessage(jid, msg)
}

func _SendImage(number types.JID, imagePath *C.char, caption *C.char) C.int {

    // type imageStruct struct {
    //     Phone       string
    //     Image       string
    //     Caption     string
    //     Id          string
    //     ContextInfo waProto.ContextInfo
    // }
        // Check if the client is connected
    if !WpClient.IsConnected() {
        err := WpClient.Connect()
        if err != nil {
            return 1
        }
    }

    // var filedata []byte
    filedata, err := os.ReadFile(C.GoString(imagePath))
    if err != nil {
        return 1
    }
    
    var uploaded whatsmeow.UploadResponse
    uploaded, err = WpClient.Upload(context.Background(), filedata, whatsmeow.MediaImage)
    if err != nil {
        return 1
    }
    // "data:image/png;base64,\""

    msg := &waProto.Message{ImageMessage: &waProto.ImageMessage{
        Caption:       proto.String(C.GoString(caption)),
        Url:           proto.String(uploaded.URL),
        DirectPath:    proto.String(uploaded.DirectPath),
        MediaKey:      uploaded.MediaKey,
        Mimetype:      proto.String(http.DetectContentType(filedata)),
        FileEncSha256: uploaded.FileEncSHA256,
        FileSha256:    uploaded.FileSHA256,
        FileLength:    proto.Uint64(uint64(len(filedata))),
    }}
    _, err = WpClient.SendMessage(context.Background(), number, msg)
    if err != nil {
        return 1
    }
    return 0
}

//export SendImage
func SendImage(number *C.char, imagePath *C.char, caption *C.char) C.int {
    var jid types.JID = assignUserJid(C.GoString(number))
    return _SendImage(jid, imagePath, caption)
}

//export SendGroupImage
func SendGroupImage(number *C.char, imagePath *C.char, caption *C.char) C.int {
    var jid types.JID = assignGroupJid(C.GoString(number))
    return _SendImage(jid, imagePath, caption)
}

// taken from https://github.com/tulir/whatsmeow/blob/main/mdtest/main.go

func handler(rawEvt interface{}) {
    if ptr_to_python_function != nil{        
        switch evt := rawEvt.(type) {
        case *events.AppStateSyncComplete:
            if len(WpClient.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
                err := WpClient.SendPresence(types.PresenceAvailable)
                if err != nil {
                    //log.Warnf("Failed to send available presence: %v", err)
                } else {
                    EventQueue.Enqueue("{\"eventType\":\"AppStateSyncComplete\",\"name\":\""+evt.Name+"\"}")
                    //log.Infof("Marked self as available")
                }
            }
        case *events.Connected:
            if len(WpClient.Store.PushName) == 0 {
                return
            }
            // Send presence available when connecting and when the pushname is changed.
            // This makes sure that outgoing messages always have the right pushname.
            err := WpClient.SendPresence(types.PresenceAvailable)
            if err != nil {
                //log.Warnf("Failed to send available presence: %v", err)
            } else {
                EventQueue.Enqueue("{\"eventType\":\"Connected\"}")
                //log.Infof("Marked self as available")
            }
        case *events.PushNameSetting:
            if len(WpClient.Store.PushName) == 0 {
                return
            }
            // Send presence available when connecting and when the pushname is changed.
            // This makes sure that outgoing messages always have the right pushname.
            err := WpClient.SendPresence(types.PresenceAvailable)
            if err != nil {
                //log.Warnf("Failed to send available presence: %v", err)
            } else {
                EventQueue.Enqueue("{\"eventType\":\"PushNameSetting\",\"timestamp\":"+strconv.FormatInt(evt.Timestamp.Unix(), 10)+",\"action\": \""+(*evt.Action.Name)+"\",\"FromFullSync\":"+strconv.FormatBool(evt.FromFullSync)+"}")
                //log.Infof("Marked self as available")
            }
        case *events.StreamReplaced:
            os.Exit(0)
        case *events.Message:
            // fmt.Println("3. Event type: Message")

            var info string
            info += "{\"id\":\""+evt.Info.ID+"\""
            info += ",\"messageSource\":\""+evt.Info.MessageSource.SourceString()+"\""
            if evt.Info.Type != "" {
                info += ",\"type\":\""+evt.Info.Type+"\""
            }
            info += ",\"pushName\":\""+evt.Info.PushName+"\""
            info += ",\"timestamp\":"+strconv.FormatInt(evt.Info.Timestamp.Unix(), 10)
            if evt.Info.Category != "" {
                info += ",\"category\":"+evt.Info.Category
            }
            info += ",\"multicast\":"+strconv.FormatBool(evt.Info.Multicast)
            if evt.Info.MediaType != "" {
                info += ",\"mediaType\": "+evt.Info.MediaType
            }
            info += ",\"flags\":["

            var flags []string
            if evt.IsEphemeral {
                flags = append(flags, "\"ephemeral\"")
            }
            if evt.IsViewOnce {
                flags = append(flags, "\"viewOnce\"")
            }
            if evt.IsViewOnceV2 {
                flags = append(flags, "\"viewOnceV2\"")
            }
            if evt.IsDocumentWithCaption {
                flags = append(flags, "\"documentWithCaption\"")
            }
            if evt.IsEdit {
                flags = append(flags, "\"edit\"")
            }
            info += strings.Join(flags, ",")
            info += "]}"

            var m, _ = protojson.Marshal(evt.Message)
            var message_info string = string(m)
            json_str := "{\"eventType\":\"Message\",\"info\":"+info+",\"message\":"+message_info+"}"
            
            EventQueue.Enqueue(json_str)
        case *events.Receipt:
            if evt.Type == types.ReceiptTypeRead || evt.Type == types.ReceiptTypeReadSelf {
                //log.Infof("%v was read by %s at %s", evt.MessageIDs, evt.SourceString(), evt.Timestamp)
            } else if evt.Type == types.ReceiptTypeDelivered {
                //log.Infof("%s was delivered to %s at %s", evt.MessageIDs[0], evt.SourceString(), evt.Timestamp)
            }
        case *events.Presence:
            var json_str string
            var online bool = !evt.Unavailable

            json_str += "{\"eventType\":\"Presence\",\"from\":\""+evt.From.String()+"\",\"online\":"+strconv.FormatBool(online)

            if evt.Unavailable {
                if !evt.LastSeen.IsZero() {
                    json_str += ",\"lastSeen\":"+strconv.FormatInt(evt.LastSeen.Unix(), 10)
                }
            }
            json_str += "}"
            EventQueue.Enqueue(json_str)

        case *events.HistorySync:
            id := atomic.AddInt32(&historySyncID, 1)
            fileName := fmt.Sprintf("history-%d-%d.json", startupTime, id)
            file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, 0600)
            if err != nil {
                //log.Errorf("Failed to open file to write history sync: %v", err)
                return
            }
            enc := json.NewEncoder(file)
            enc.SetIndent("", "  ")
            err = enc.Encode(evt.Data)
            if err != nil {
                //log.Errorf("Failed to write history sync: %v", err)
                return
            }
            //log.Infof("Wrote history sync to %s", fileName)
            _ = file.Close()

            EventQueue.Enqueue("{\"eventType\":\"HistorySync\",\"filename\":\""+fileName+"\"}")
        case *events.AppState:
            //log.Debugf("App state event: %+v / %+v", evt.Index, evt.SyncActionValue)
            var json_string string = "{\"eventType\":\"AppState\",\"index\":\"["
            for _, value := range evt.Index {
                json_string += "\""+value+"\""
            }
            json_string += "],\"syncActionValue\":"+evt.SyncActionValue.String()+"}"
            EventQueue.Enqueue(json_string)
            
        case *events.KeepAliveTimeout:
            //log.Debugf("Keepalive timeout event: %+v", evt)
            var json_string string = "{\"eventType\":\"KeepAliveTimeout\",\"errorCount\":"+strconv.FormatInt(int64(evt.ErrorCount), 10)+",\"lastSuccess\":"+strconv.FormatInt(evt.LastSuccess.Unix(), 10)+"}"
            EventQueue.Enqueue(json_string)
        case *events.KeepAliveRestored:
            //log.Debugf("Keepalive restored")
            EventQueue.Enqueue("{\"eventType\":\"KeepAliveRestored\"}")
        case *events.Blocklist:
            EventQueue.Enqueue("{\"eventType\":\"Blocklist\"}")
            //log.Infof("Blocklist event: %+v", evt)
        default:
            // fmt.Println("Missing event")
            // fmt.Printf("I don't know about type %T!\n", evt)

        }
    }
}

var ptr_to_python_function C.ptr_to_python_function

//export HandlerThread
func HandlerThread(fn C.ptr_to_python_function) {
    ptr_to_python_function = fn
    for{
        // fmt.Println("1 - Waiting for next enqueued element")
        value, _ := EventQueue.DequeueOrWaitForNextElement()
        // fmt.Printf("2 - Dequeued element: %v\n", value)
        // fmt.Println(ptr_to_python_function)

        var str_value = value.(string)
        var cstr = C.CString(str_value)

        C.call_c_func(ptr_to_python_function, cstr)
        C.free(unsafe.Pointer(cstr))
        // fmt.Printf("3 - DONE")
    }
}

func main() {
}
