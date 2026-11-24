package reporter

import (
	"encoding/xml"
	"fmt"
	"time"
)

type junitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Time      string          `xml:"time,attr"`
	Timestamp string          `xml:"timestamp,attr"`
	Cases     []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Details string `xml:",chardata"`
}

// RenderJUnit serializes ReportData into JUnit XML compatible with GitHub Actions, GitLab CI, and Jenkins.
func RenderJUnit(data ReportData) ([]byte, error) {
	ts := data.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	durSec := fmt.Sprintf("%.3f", data.Duration.Seconds())
	classname := "tensgo"
	if data.Target != "" {
		classname = "tensgo." + data.Target
	}

	var cases []junitTestCase
	failures := 0

	if len(data.Gates) == 0 {
		cases = append(cases, junitTestCase{
			Name:      "Load Test Execution",
			Classname: classname,
			Time:      durSec,
		})
	} else {
		for _, gate := range data.Gates {
			tc := junitTestCase{
				Name:      gate.Name,
				Classname: classname,
				Time:      durSec,
			}
			if !gate.Passed {
				failures++
				msg := gate.Message
				if msg == "" {
					msg = fmt.Sprintf("expected %s, got %s", gate.Expected, gate.Actual)
				}
				tc.Failure = &junitFailure{
					Message: msg,
					Type:    "QualityGateViolation",
					Details: fmt.Sprintf("Gate: %s\nExpected: %s\nActual: %s", gate.Name, gate.Expected, gate.Actual),
				}
			}
			cases = append(cases, tc)
		}
	}

	suite := junitTestSuite{
		Name:      "tensgo Quality Gates",
		Tests:     len(cases),
		Failures:  failures,
		Time:      durSec,
		Timestamp: ts.UTC().Format(time.RFC3339),
		Cases:     cases,
	}

	root := junitTestSuites{
		Name:     "tensgo",
		Tests:    len(cases),
		Failures: failures,
		Time:     durSec,
		Suites:   []junitTestSuite{suite},
	}

	out, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}
