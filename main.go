/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	ecc "github.com/ernestio/ernest-config-client"
	"github.com/nats-io/nats"
)

var nc *nats.Conn
var natsErr error

func eventHandler(m *nats.Msg) {
	var e Event

	err := e.Process(m.Data)
	if err != nil {
		println(err.Error())
		return
	}

	if err = e.Validate(); err != nil {
		e.Error(err)
		return
	}

	parts := strings.Split(m.Subject, ".")
	switch parts[1] {
	case "create":
		err = createS3(&e)
	case "update":
		err = updateS3(&e)
	case "delete":
		err = deleteS3(&e)
	}

	if err != nil {
		e.Error(err)
		return
	}

	e.Complete()
}

func createS3(ev *Event) error {
	s3client := getS3Client(ev)

	params := &s3.CreateBucketInput{
		Bucket: aws.String(ev.Name),
		ACL:    aws.String(ev.Acl),
		CreateBucketConfiguration: &s3.CreateBucketConfiguration{
			LocationConstraint: aws.String(ev.BucketLocation),
		},
	}

	resp, err := s3client.CreateBucket(params)
	if err != nil {
		return err
	}

	req := s3.HeadBucketInput{
		Bucket: resp.Location,
	}

	err = s3client.WaitUntilBucketExists(&req)
	if err != nil {
		return err
	}

	ev.BucketURI = *resp.Location

	err = updateS3(ev)

	return err
}

func updateS3(ev *Event) error {
	s3client := getS3Client(ev)
	params := &s3.PutBucketAclInput{
		Bucket: aws.String(ev.BucketURI),
		ACL:    aws.String(ev.Acl),
	}

	var grants []*s3.Grant
	for _, g := range ev.Grantees {
		grantee := s3.Grantee{
			Type: aws.String(g.Type),
		}
		switch g.Type {
		case "id":
			grantee.ID = aws.String(g.ID)
		case "emailaddress":
			grantee.EmailAddress = aws.String(g.ID)
		case "uri":
			grantee.URI = aws.String(g.ID)
		}

		grants = append(grants, &s3.Grant{
			Grantee:    &grantee,
			Permission: aws.String(g.Permissions),
		})
	}
	_, err := s3client.PutBucketAcl(params)

	return err
}

func deleteS3(ev *Event) error {
	s3client := getS3Client(ev)
	params := &s3.DeleteBucketInput{
		Bucket: aws.String(ev.Name),
	}
	_, err := s3client.DeleteBucket(params)

	return err
}

func getS3Client(ev *Event) *s3.S3 {
	creds := credentials.NewStaticCredentials(ev.DatacenterSecret, ev.DatacenterToken, "")
	s3client := s3.New(session.New(), &aws.Config{
		Region:      aws.String(ev.DatacenterRegion),
		Credentials: creds,
	})
	return s3client
}

func main() {
	nc = ecc.NewConfig(os.Getenv("NATS_URI")).Nats()

	fmt.Println("listening for s3.create.aws")
	nc.Subscribe("s3.create.aws", eventHandler)

	fmt.Println("listening for s3.update.aws")
	nc.Subscribe("s3.update.aws", eventHandler)

	fmt.Println("listening for s3.delete.aws")
	nc.Subscribe("s3.delete.aws", eventHandler)

	runtime.Goexit()
}
