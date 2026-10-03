entity "Artist" {
  key = ["ArtistId"]
  property "ArtistId" { type = "int" }
  property "Name" { type = "string" }
}

entity "Album" {
  key = ["AlbumId"]
  property "AlbumId" { type = "int" }
  property "Title" { type = "string" }
  property "ArtistId" { entity = "Artist" }
}
entity "propertyIsEnumerable" {
  key = ["Id"]
  property "Id" { type = "int" }
}
