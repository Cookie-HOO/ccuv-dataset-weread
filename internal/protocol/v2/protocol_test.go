package v2

import "testing"

func TestDecodeAcceptsForwardCompatibleV2Chart(t *testing.T) {
	request, err := Decode([]byte(`{"protocol":"ccuv.custom/v2","request_id":"pane-1:2","request_kind":"chart","dataset_id":"weread","metric_id":"reading_time","timezone":"Asia/Shanghai","range":{"start_date":"2026-09-01","end_date":"2026-09-30","period":"1mo","future":true},"query":{"filters":{},"group_by":null,"granularity":"day","future":true}}`))
	if err != nil || request.Chart == nil || request.Chart.Range.Period == nil || *request.Chart.Range.Period != "1mo" {
		t.Fatalf("request = %#v, err = %v", request, err)
	}
}

func TestDecodeRejectsV1AndRecoversTrustedV2Envelope(t *testing.T) {
	if _, err := Decode([]byte(`{"protocol":"ccuv.custom/v1"}`)); err == nil {
		t.Fatal("accepted V1")
	}
	response, ok := InvalidRequestFor([]byte(`{"protocol":"ccuv.custom/v2","request_id":"pane-1:2","request_kind":"chart","dataset_id":"weread","metric_id":"reading_time"}`))
	if !ok || response.Error == nil || response.Error.Code != "invalid_request" || response.RequestID != "pane-1:2" {
		t.Fatalf("response = %#v, ok = %v", response, ok)
	}
}

func TestDecodeRejectsTrailingJSON(t *testing.T) {
	if _, err := Decode([]byte(`{} {}`)); err == nil {
		t.Fatal("accepted multiple JSON values")
	}
}
