package delivery

// RefusalHeader identifies a completed refusal from the canonical delivery
// operation on the client's authenticated HTTPS origin. A generic router or
// proxy 404 is not authority to erase a previously delivered Secret.
const RefusalHeader = "X-Hikyo-Delivery-Refusal"

const RefusalVersion = "v1"
