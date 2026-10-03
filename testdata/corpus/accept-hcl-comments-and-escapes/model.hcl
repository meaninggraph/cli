# one
// two
/* three
   lines */
entity "Artist" {
  key = ["ArtistId"]
  property "ArtistId" { type = "int" }
  property "Name" {
    type = "string"
    pattern = "a\tb\"c"
    required = true
    max_len = 10
  }
}

entity "Album" {
  key = ["AlbumId"]
  property "AlbumId" { type = "int" }
  property "Title" { type = "string" }
  property "ArtistId" { entity = "Artist" }
  property "Sales" { type = "decimal" }
}
