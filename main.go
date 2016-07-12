/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nats-io/nats"
)

var nc *nats.Conn
var natsErr error

func processEvent(data []byte) (*Event, error) {
	var ev Event
	err := json.Unmarshal(data, &ev)
	return &ev, err
}

func eventHandler(m *nats.Msg) {
	i, err := processEvent(m.Data)
	if err != nil {
		nc.Publish("instance.update.aws.error", m.Data)
		return
	}

	if i.Valid() == false {
		i.Error(errors.New("Instance is invalid"))
		return
	}

	err = updateInstance(i)
	if err != nil {
		i.Error(err)
		return
	}

	i.Complete()
}

func updateInstance(ev *Event) error {
	creds := credentials.NewStaticCredentials(ev.DatacenterAccessKey, ev.DatacenterAccessToken, "")
	svc := ec2.New(session.New(), &aws.Config{
		Region:      aws.String(ev.DatacenterRegion),
		Credentials: creds,
	})

	req := ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(ev.InstanceAWSID),
		InstanceType: &ec2.AttributeValue{
			Value: aws.String(ev.InstanceType),
		},
	}

	_, err := svc.ModifyInstanceAttribute(&req)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	natsURI := os.Getenv("NATS_URI")
	if natsURI == "" {
		natsURI = nats.DefaultURL
	}

	nc, natsErr = nats.Connect(natsURI)
	if natsErr != nil {
		log.Fatal(natsErr)
	}

	fmt.Println("listening for instance.update.aws")
	nc.Subscribe("instance.update.aws", eventHandler)

	runtime.Goexit()
}
