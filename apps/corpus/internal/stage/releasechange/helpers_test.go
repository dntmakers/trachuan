package releasechange

const testRel = "20990102_1"
const previousRel = "20990101_1"

func bareSHA256(data []byte) string { return sha(string(data)) }
