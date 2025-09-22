package entityserver_v1alpha

// EntityOperation represents the type of operation on an entity
type EntityOperation int64

const (
	// EntityOperationCreate indicates an entity was created
	EntityOperationCreate EntityOperation = 1
	// EntityOperationUpdate indicates an entity was updated
	EntityOperationUpdate EntityOperation = 2
	// EntityOperationDelete indicates an entity was deleted
	EntityOperationDelete EntityOperation = 3
)

// OperationType returns the typed operation for this EntityOp
func (v *EntityOp) OperationType() EntityOperation {
	return EntityOperation(v.Operation())
}

// IsCreate returns true if this is a create operation
func (v *EntityOp) IsCreate() bool {
	return v.Operation() == int64(EntityOperationCreate)
}

// IsUpdate returns true if this is an update operation
func (v *EntityOp) IsUpdate() bool {
	return v.Operation() == int64(EntityOperationUpdate)
}

// IsDelete returns true if this is a delete operation
func (v *EntityOp) IsDelete() bool {
	return v.Operation() == int64(EntityOperationDelete)
}
