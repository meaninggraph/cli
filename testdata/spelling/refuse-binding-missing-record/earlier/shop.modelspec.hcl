entity "Customer" {
  key = ["CustomerId"]
  property "CustomerId" { type = "int" }
  property "Name" { type = "string" }
}

entity "Invoice" {
  key = ["InvoiceId"]
  property "InvoiceId" { type = "int" }
  property "Total" { type = "decimal" }
  property "CustomerId" { entity = "Customer" }
}
