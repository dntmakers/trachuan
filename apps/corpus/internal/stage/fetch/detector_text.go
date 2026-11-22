package fetch

import "fmt"

func DecodeDetectorTextV1(data []byte, expectedSource string) (DetectorInputsV1, error) {
	fields, err := detectorObjectMode(data, true)
	if err != nil {
		return DetectorInputsV1{}, fmt.Errorf("decode extract text: %w", err)
	}
	source, err := lineageString(fields, "text_source")
	if err != nil {
		return DetectorInputsV1{}, err
	}
	if source != expectedSource {
		return DetectorInputsV1{}, fmt.Errorf("extract text source mismatch")
	}
	return DecodeDetectorInputsV1(fields["detector_inputs"], source)
}
