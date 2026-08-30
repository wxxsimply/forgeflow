package checkpoint

import fixture "forgeflow-eval-fixture"

type Checkpoint = fixture.Checkpoint

func SaveCheckpoint(value Checkpoint) ([]byte, error) { return fixture.SaveCheckpoint(value) }

func LoadCheckpoint(data []byte) (Checkpoint, error) { return fixture.LoadCheckpoint(data) }
