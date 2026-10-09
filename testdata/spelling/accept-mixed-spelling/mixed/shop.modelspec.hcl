entity "Customer" {
  key = ["CustomerId"]
  field "CustomerId" { type = "int" }
  property "Name" { type = "string" }
}

record "Invoice" {
  key = ["InvoiceId"]
  property "InvoiceId" { type = "int" }
  field "Total" { type = "decimal" }
  field "CustomerId" { entity = "Customer" }
  field "OtherCustomerId" { record = "Customer" }
}
