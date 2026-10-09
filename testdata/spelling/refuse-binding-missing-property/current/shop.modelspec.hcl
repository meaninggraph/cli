record "Customer" {
  key = ["CustomerId"]
  field "CustomerId" { type = "int" }
  field "Name" { type = "string" }
}

record "Invoice" {
  key = ["InvoiceId"]
  field "InvoiceId" { type = "int" }
  field "Total" { type = "decimal" }
  field "CustomerId" { record = "Customer" }
}
