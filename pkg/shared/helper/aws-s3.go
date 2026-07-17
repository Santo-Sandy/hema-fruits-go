package helper

import (
	"net/url"
	"time"

	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

var api_key = utils.GetenvStr("S3_API_KEY")
var secret = utils.GetenvStr("S3_SECRET")
var endpoint = utils.GetenvStr("S3_ENDPOINT")
var region = utils.GetenvStr("S3_REGION")

var s3Config = &aws.Config{
	Credentials: credentials.NewStaticCredentials(api_key, secret, ""),
	Endpoint:    aws.String(endpoint),
	Region:      aws.String(region),
}
var newSession = session.New(s3Config)
var s3Client = s3.New(newSession)

func CreateBucket(name string) bool {
	params := &s3.CreateBucketInput{
		Bucket: aws.String(name),
	}
	_, err := s3Client.CreateBucket(params)
	if err != nil {
		fmt.Println(err.Error())
		return false
	}
	return true
}

func ListBuckets() interface{} {
	spaces, err := s3Client.ListBuckets(nil)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}
	return spaces.Buckets
	// for _, b := range spaces.Buckets {
	//     fmt.Println(aws.StringValue(b.Name))
	// }
}

func ListBucketFiles(bucketName string) map[string]interface{} {
	input := &s3.ListObjectsInput{
		Bucket: aws.String(bucketName),
	}
	objects, err := s3Client.ListObjects(input)
	if err != nil {
		return nil
		//fmt.Println(err.Error())
	}

	result := make(map[string]interface{})
	for _, obj := range objects.Contents {
		result[*obj.Key] = obj
		//fmt.Println(aws.StringValue(obj.Key))
	}
	return result
}

func GetDownloadUrl(bucketName string, fileName string) string {
	req, _ := s3Client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fileName),
	})
	urlStr, err := req.Presign(5 * time.Minute)
	if err != nil {
		return ""
		//fmt.Println(err.Error())
	}
	return urlStr
}

func GetUploadUrl(bucketName string, fileName string, metaData map[string]*string) string {
	req, _ := s3Client.PutObjectRequest(&s3.PutObjectInput{
		Bucket:   aws.String(bucketName),
		Key:      aws.String(fileName),
		Metadata: metaData,
	})
	urlStr, err := req.Presign(5 * time.Minute)
	if err != nil {
		fmt.Println(err.Error())
		return ""
	}
	return urlStr
}

func DeleteFile(bucketName string, fileName string) bool {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fileName),
	}
	result, err := s3Client.DeleteObject(input)
	if err != nil {
		fmt.Println(err.Error())
		return false
	}
	return *result.DeleteMarker
}

func CopyFile(sourceBucketName string, targetBucketName string, filename string) bool {
	input := &s3.CopyObjectInput{
		Bucket:     aws.String(targetBucketName),
		CopySource: aws.String(url.PathEscape(sourceBucketName + "/" + filename)),
		Key:        aws.String(filename),
	}
	_, err := s3Client.CopyObject(input)
	if err != nil {
		fmt.Println(err.Error())
		return false
	}
	return true
}

func MoveFile(sourceBucketName string, targetBucketName string, filename string) bool {
	if CopyFile(sourceBucketName, targetBucketName, filename) {
		return DeleteFile(sourceBucketName, filename)
	}
	return false
}
