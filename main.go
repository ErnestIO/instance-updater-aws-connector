/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

package main

import (
	"errors"
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

func getInstanceByID(svc *ec2.EC2, id string) (*ec2.Instance, error) {
	req := ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(id)},
	}

	resp, err := svc.DescribeInstances(&req)
	if err != nil {
		return nil, err
	}

	if len(resp.Reservations) != 1 {
		return nil, errors.New("Could not find any instance reservations")
	}

	if len(resp.Reservations[0].Instances) != 1 {
		return nil, errors.New("Could not find an instance with that ID")
	}

	return resp.Reservations[0].Instances[0], nil
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

	// power off the instance
	_, err = svc.StopInstances(&stopreq)
	if err != nil {
		return err
	}

	err = svc.WaitUntilInstanceStopped(&builtInstance)
	if err != nil {
		return err
	}

	// resize the instance
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

	// update instance security groups
	req = ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(ev.InstanceAWSID),
		Groups:     []*string{},
	}

	for _, sg := range ev.SecurityGroupAWSIDs {
		req.Groups = append(req.Groups, aws.String(sg))
	}

	_, err = svc.ModifyInstanceAttribute(&req)
	if err != nil {
		return err
	}

	// power the instance back on
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

	instance, err := getInstanceByID(svc, ev.InstanceAWSID)
	if err != nil {
		return err
	}

	if instance.PublicIpAddress != nil {
		ev.PublicIP = *instance.PublicIpAddress
	}

	return nil
}

func main() {
	nc = ecc.NewConfig(os.Getenv("NATS_URI")).Nats()

	fmt.Println("listening for instance.update.aws")
	nc.Subscribe("instance.update.aws", eventHandler)

	runtime.Goexit()
}
