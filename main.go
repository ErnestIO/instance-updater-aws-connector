/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	ecc "github.com/ernestio/ernest-config-client"
	"github.com/nats-io/nats"
)

var nc *nats.Conn
var natsErr error

func eventHandler(m *nats.Msg) {
	var i Event

	err := i.Process(m.Data)
	if err != nil {
		return
	}

	if err = i.Validate(); err != nil {
		i.Error(err)
		return
	}

	err = updateInstance(&i)
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

	builtInstance := ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(ev.InstanceAWSID)},
	}

	okInstance := ec2.DescribeInstanceStatusInput{
		InstanceIds: []*string{aws.String(ev.InstanceAWSID)},
	}

	err := svc.WaitUntilInstanceStatusOk(&okInstance)
	if err != nil {
		return err
	}

	stopreq := ec2.StopInstancesInput{
		InstanceIds: []*string{aws.String(ev.InstanceAWSID)},
	}

	_, err = svc.StopInstances(&stopreq)
	if err != nil {
		return err
	}

	err = svc.WaitUntilInstanceStopped(&builtInstance)
	if err != nil {
		return err
	}

	req := ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(ev.InstanceAWSID),
		InstanceType: &ec2.AttributeValue{
			Value: aws.String(ev.InstanceType),
		},
	}

	_, err = svc.ModifyInstanceAttribute(&req)
	if err != nil {
		return err
	}

	startreq := ec2.StartInstancesInput{
		InstanceIds: []*string{aws.String(ev.InstanceAWSID)},
	}

	_, err = svc.StartInstances(&startreq)
	if err != nil {
		return err
	}

	err = svc.WaitUntilInstanceRunning(&builtInstance)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	nc = ecc.NewConfig(os.Getenv("NATS_URI")).Nats()

	fmt.Println("listening for instance.update.aws")
	nc.Subscribe("instance.update.aws", eventHandler)

	runtime.Goexit()
}
