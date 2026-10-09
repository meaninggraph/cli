record "Customer" {
  key = ["CustomerId"]
  field "CustomerId" { type = "int" }
}

record "Invoice" {
  field "InvoiceId" { type = "int" }
  field "CustomerId" {
    entity = "Customer"
    record = "Customer"
  }
}
