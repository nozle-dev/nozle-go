package nozle

import (
 "context"
 "crypto/rand"
 "fmt"
 "net/url"
)
// Track records a Core metered event. Supply a stable TransactionID when retrying.
func(c *Client) Track(ctx context.Context,p TrackParams) error {
 if err:=validateKey(c.apiKey,"track");err!=nil{return err};if err:=required("track","customer_id",p.CustomerID);err!=nil{return err};if err:=required("track","event",p.Event);err!=nil{return err}
 sub:=p.SubscriptionID
 if sub=="" {var found struct{Subscriptions []struct{ExternalID string `json:"external_id"`} `json:"subscriptions"`};q:=url.Values{"external_customer_id":{p.CustomerID},"status[]":{"active"}};if err:=c.request(ctx,"track.subscription_lookup","GET",true,query("/api/v1/subscriptions",q),nil,&found,"");err!=nil{return err};if len(found.Subscriptions)!=1{return &ValidationError{"track","exactly one active subscription required; supply SubscriptionID"}};sub=found.Subscriptions[0].ExternalID}
 id:=p.TransactionID;if id==""{id=generateUUID()};properties:=p.Metadata;if properties==nil{properties=map[string]any{}}
 event:=map[string]any{"transaction_id":id,"external_customer_id":p.CustomerID,"external_subscription_id":sub,"code":p.Event,"properties":properties};if p.Timestamp!=""{event["timestamp"]=p.Timestamp}
 return c.request(ctx,"track","POST",true,"/api/v1/events",map[string]any{"event":event},nil,"")
}
func generateUUID() string { var b [16]byte; if _,err:=rand.Read(b[:]);err!=nil{panic(err)};b[6]=(b[6]&15)|64;b[8]=(b[8]&63)|128;return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",b[0:4],b[4:6],b[6:8],b[8:10],b[10:16]) }
